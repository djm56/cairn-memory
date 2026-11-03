package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/core"
)

// operatorRouteList is every call only an operator key may make (CONTRACT §8): the routes the
// route table marks OperatorOnly, read from api.Routes. It is derived, not typed out, so a call
// added to the table is tested here without a change to this file. The test below fails on an
// empty list, since an empty list would pass every check.
var operatorRouteList = func() []string {
	var out []string
	for _, r := range api.Routes() {
		if r.OperatorOnly {
			out = append(out, r.Route)
		}
	}
	return out
}()

// TestOperatorRouteRefusesAnAgentBeforeTheBody checks the order of CONTRACT §1.2 on
// the wire: the role is refused before the body is decoded. An agent key that sends a
// malformed body, an unknown field, a body that is not JSON, an oversized body (413
// for the decoder) or a body of the wrong type (400 for the decoder) to an operator
// route gets 403 operator_only, not 400, 413 or 415 (#144). The operator key with the
// same malformed body gets 400, so the decoder still runs for a caller that may make
// the call. The key is stored and resolved as on a served call, not stubbed.
func TestOperatorRouteRefusesAnAgentBeforeTheBody(t *testing.T) {
	st := openKeyStore(t)
	h := keyedHandler(st, core.NewLocal(st))
	agent := addKey(t, st, "ci", auth.ScopeAgent)
	operator := addKey(t, st, "ops", auth.ScopeOperator)
	if len(operatorRouteList) == 0 {
		t.Fatal("the route table marks no operator route: the list is empty, so nothing is tested")
	}

	bodies := []struct {
		what        string
		body        string
		contentType string
	}{
		{"a malformed body", `{"code":`, "application/json"},
		{"an unknown field", `{"no_such_field":1}`, "application/json"},
		{"a body that is not JSON", `code=D-0001`, "text/plain"},
		{"an oversized body", `{"code":"` + strings.Repeat("x", api.MaxBodyBytes) + `"}`, "application/json"},
		{"a body of the wrong type", `[1,2]`, "application/json"},
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

// readerRouteList is the four web UI reads of CONTRACT §8.1, by their route constants.
var readerRouteList = []string{
	api.RouteProjectList,
	api.RouteTaskBrowse,
	api.RouteDirectiveBrowse,
	api.RouteDashboard,
}

// TestReaderRouteRefusesAnAgentBeforeTheBody checks the order of CONTRACT §1.2 and §8.1 on the
// web UI reads: an agent key is refused with operator_only before the body is read, for every
// body shape. A viewer key passes the role gate, so the same bodies reach the decoder and get
// the decoder's answer: 400 for a malformed body, an unknown field or a body of the wrong type,
// 413 for an oversized body, and 415 for a body that is not JSON. An operator key passes too.
func TestReaderRouteRefusesAnAgentBeforeTheBody(t *testing.T) {
	st := openKeyStore(t)
	h := keyedHandler(st, core.NewLocal(st))
	agent := addKey(t, st, "ci", auth.ScopeAgent)
	viewer := addKey(t, st, "watch", auth.ScopeViewer)

	bodies := []struct {
		what        string
		body        string
		contentType string
		viewerCode  int
	}{
		{"a malformed body", `{"code":`, "application/json", http.StatusBadRequest},
		{"an unknown field", `{"no_such_field":1}`, "application/json", http.StatusBadRequest},
		{"a body that is not JSON", `code=D-0001`, "text/plain", http.StatusUnsupportedMediaType},
		{"an oversized body", `{"code":"` + strings.Repeat("x", api.MaxBodyBytes) + `"}`, "application/json", http.StatusRequestEntityTooLarge},
		{"a body of the wrong type", `[1,2]`, "application/json", http.StatusBadRequest},
	}
	for _, route := range readerRouteList {
		for _, b := range bodies {
			hdr := bearer(agent)
			hdr["Content-Type"] = b.contentType
			got := serve(h, http.MethodPost, api.Prefix+route, b.body, hdr)
			if got.Code != http.StatusForbidden {
				t.Errorf("agent key on %s with %s: status %d, want 403 before the body is read: %s",
					route, b.what, got.Code, got.Body.String())
			} else if e := errorBody(t, got); e.Kind != core.KindOperatorOnly {
				t.Errorf("agent key on %s with %s: kind %s, want operator_only", route, b.what, e.Kind)
			}

			hdr = bearer(viewer)
			hdr["Content-Type"] = b.contentType
			got = serve(h, http.MethodPost, api.Prefix+route, b.body, hdr)
			if got.Code != b.viewerCode {
				t.Errorf("viewer key on %s with %s: status %d, want %d from the decoder: %s",
					route, b.what, got.Code, b.viewerCode, got.Body.String())
			}
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
