package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/core"
)

// operatorRoutes are the four calls of CONTRACT §8, by their route constants.
var operatorRouteList = []string{
	api.RouteLearningDecide,
	api.RouteDirectiveRatify,
	api.RouteDirectiveRetire,
	api.RouteProjectSettingsSet,
}

// TestOperatorRouteRefusesAnAgentBeforeTheBody checks the order of CONTRACT §1.2 on
// the wire: the role is refused before the body is decoded. An agent key that sends a
// malformed body, an unknown field, or a body that is not JSON to an operator route gets
// 403 operator_only, not 400 or 415. The operator key with the same body gets 400, so
// the decoder still runs for a caller that may make the call. The key is stored and
// resolved as on a served call, not stubbed.
func TestOperatorRouteRefusesAnAgentBeforeTheBody(t *testing.T) {
	st := openKeyStore(t)
	h := keyedHandler(st, core.NewLocal(st))
	agent := addKey(t, st, "ci", auth.ScopeAgent)
	operator := addKey(t, st, "ops", auth.ScopeOperator)

	bodies := []struct {
		what        string
		body        string
		contentType string
	}{
		{"a malformed body", `{"code":`, "application/json"},
		{"an unknown field", `{"no_such_field":1}`, "application/json"},
		{"a body that is not JSON", `code=D-0001`, "text/plain"},
	}
	for _, route := range operatorRouteList {
		for _, b := range bodies {
			hdr := bearer(agent)
			hdr["Content-Type"] = b.contentType
			got := serve(h, http.MethodPost, api.Prefix+route, b.body, hdr)
			if got.Code != http.StatusForbidden {
				t.Errorf("agent key on %s with %s: status %d, want 403 before the body is read: %s",
					route, b.what, got.Code, got.Body.String())
				continue
			}
			if e := errorBody(t, got); e.Kind != core.KindOperatorOnly {
				t.Errorf("agent key on %s with %s: kind %s, want operator_only", route, b.what, e.Kind)
			}
		}

		hdr := bearer(operator)
		hdr["Content-Type"] = "application/json"
		got := serve(h, http.MethodPost, api.Prefix+route, `{"code":`, hdr)
		if got.Code != http.StatusBadRequest {
			t.Errorf("operator key on %s with a malformed body: status %d, want 400 from the decoder: %s",
				route, got.Code, got.Body.String())
		}
	}
}

// TestUnauthenticatedAnswerNamesTheBearerScheme checks RFC 7235 §3.1 on a 401: it names
// the scheme the server takes, and the answer still does not echo the credential.
func TestUnauthenticatedAnswerNamesTheBearerScheme(t *testing.T) {
	st := openKeyStore(t)
	h := keyedHandler(st, core.NewLocal(st))
	hdr := map[string]string{api.HeaderProject: "acme-api", api.HeaderAgent: "main"}
	got := serve(h, http.MethodPost, api.Prefix+api.RouteTaskCurrent, "", hdr)
	if got.Code != http.StatusUnauthorized {
		t.Fatalf("no key: status %d, want 401: %s", got.Code, got.Body.String())
	}
	if w := got.Header().Get("WWW-Authenticate"); w != "Bearer" {
		t.Errorf("401 WWW-Authenticate = %q, want %q", w, "Bearer")
	}

	hdr["Authorization"] = "Basic dXNlcjpwYXNz"
	got = serve(h, http.MethodPost, api.Prefix+api.RouteTaskCurrent, "", hdr)
	if got.Header().Get("WWW-Authenticate") != "Bearer" || strings.Contains(got.Body.String(), "dXNlcjpwYXNz") {
		t.Errorf("401 for another scheme: WWW-Authenticate %q, or the answer echoes the credential",
			got.Header().Get("WWW-Authenticate"))
	}
}
