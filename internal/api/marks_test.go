package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/core"
)

// expectedRoute is one route's access and role, written out by hand from CONTRACT §2
// to §8 and DESIGN §5.4. It is deliberately not derived from routeAccess or
// operatorRoutes: a mark flipped in either map must fail against this table.
type expectedRoute struct {
	route    string
	access   access
	operator bool
}

// expectedRoutes is every call of the wire form: the 15 agent calls, then the four
// operator calls, which are write routes that need the operator role.
var expectedRoutes = []expectedRoute{
	{RouteTaskCurrent, accessRead, false},
	{RouteTaskStart, accessWrite, false},
	{RouteTaskGet, accessRead, false},
	{RouteTaskList, accessRead, false},
	{RouteTaskUpdate, accessWrite, false},
	{RouteTaskClose, accessWrite, false},
	{RouteStepPlan, accessWrite, false},
	{RouteStepAdd, accessWrite, false},
	{RouteStepUpdate, accessWrite, false},
	{RouteEventLog, accessWrite, false},
	{RouteLearningAdd, accessWrite, false},
	{RouteLearningList, accessRead, false},
	{RouteDirectiveList, accessRead, false},
	{RouteRecall, accessRead, false},
	{RouteSearch, accessRead, false},
	{RouteLearningDecide, accessWrite, true},
	{RouteDirectiveRatify, accessWrite, true},
	{RouteDirectiveRetire, accessWrite, true},
	{RouteProjectSettingsSet, accessWrite, true},
}

// TestRouteMarksMatchTheExpectedTable compares every mark with the table. It fails when
// a route is marked the wrong way, when a route is marked but missing from the table, or
// when the table names an operator route the marks do not have. TestEveryRouteIsGatedByItsMark
// checks the same marks through the wire, so a flipped mark is caught twice.
func TestRouteMarksMatchTheExpectedTable(t *testing.T) {
	if len(routeAccess) != len(expectedRoutes) {
		t.Fatalf("routeAccess has %d routes, the expected table %d: add a new route to both",
			len(routeAccess), len(expectedRoutes))
	}
	for _, e := range expectedRoutes {
		got, ok := routeAccess[e.route]
		if !ok || got != e.access {
			t.Errorf("%s: access mark %v, want %v", e.route, got, e.access)
		}
		if operatorRoutes[e.route] != e.operator {
			t.Errorf("%s: operator mark %v, want %v", e.route, operatorRoutes[e.route], e.operator)
		}
		if e.operator && e.access != accessWrite {
			t.Errorf("%s: the table names an operator route that is not a write", e.route)
		}
	}
	if want := 4; len(operatorRoutes) != want {
		t.Errorf("operatorRoutes has %d routes, want %d (CONTRACT §8)", len(operatorRoutes), want)
	}
}

// scopeLookup answers every hash with one active key of scope, so a test can send a
// call as a key of any role without a store.
type scopeLookup struct {
	scope auth.Scope
}

func (l scopeLookup) FindActive(context.Context, string) (auth.Key, error) {
	return auth.Key{ID: 1, Name: "test-" + string(l.scope), Scope: l.scope}, nil
}

func (scopeLookup) Touch(context.Context, int64, time.Time) error { return nil }

// handlerFor returns the handler over core.Unimplemented, which answers every call
// with a plain error, so a call that reaches core answers 500 internal. The key
// resolver gives every request a key of scope.
func handlerFor(scope auth.Scope) http.Handler {
	return Handler(core.Unimplemented{}, KeyCaller(auth.NewResolver(scopeLookup{scope}, nil)), nil)
}

// callRoute sends a POST of route with body to h, with a well-formed key and the
// Cairn headers of an agent call, and returns the answer.
func callRoute(h http.Handler, route, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, Prefix+route, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderProject, "acme-api")
	req.Header.Set(HeaderAgent, "main")
	req.Header.Set("Authorization", "Bearer cairn_"+strings.Repeat("A", 43))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestEveryRouteIsGatedByItsMark sends each route to the gates with each role and checks
// the answer its mark earns. A call that reaches core answers 500 internal, since core
// is unimplemented here, so a 403 or 400 shows the gate answered before core:
//   - a viewer on every write route gets 403 read_only;
//   - a viewer on every read route reaches core, so it is not refused for its scope;
//   - an agent on an operator route with a malformed body gets 403 operator_only, not 400,
//     since the role is refused before the body is decoded;
//   - an operator with the same malformed body gets 400, so the body is still decoded.
func TestEveryRouteIsGatedByItsMark(t *testing.T) {
	const malformed = `{"code":`
	viewer := handlerFor(auth.ScopeViewer)
	agent := handlerFor(auth.ScopeAgent)
	operator := handlerFor(auth.ScopeOperator)
	for _, e := range expectedRoutes {
		if e.access == accessWrite {
			rec := callRoute(viewer, e.route, "")
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"read_only"`) {
				t.Errorf("viewer on write %s: status %d, want 403 read_only: %s", e.route, rec.Code, rec.Body.String())
			}
		} else {
			rec := callRoute(viewer, e.route, "")
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("viewer on read %s: status %d, want 500 from the unimplemented core, so it was not refused for its scope: %s",
					e.route, rec.Code, rec.Body.String())
			}
		}

		if e.operator {
			rec := callRoute(agent, e.route, malformed)
			if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"operator_only"`) {
				t.Errorf("agent with a malformed body on operator %s: status %d, want 403 operator_only: %s",
					e.route, rec.Code, rec.Body.String())
			}
			rec = callRoute(operator, e.route, malformed)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("operator with a malformed body on %s: status %d, want 400 from the decoder: %s",
					e.route, rec.Code, rec.Body.String())
			}
		}
	}
}
