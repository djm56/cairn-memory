package api_test

import (
	"net/http"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/core"
)

// wirePaths are the 19 paths of DESIGN.md §5.4, written out as literals. They
// are deliberately not taken from the api.Route constants: a client codes against
// these strings, so a change to a constant must fail here, not follow it.
var wirePaths = []string{
	"/v1/agent/task_current",
	"/v1/agent/task_start",
	"/v1/agent/task_get",
	"/v1/agent/task_list",
	"/v1/agent/task_update",
	"/v1/agent/task_close",
	"/v1/agent/step_plan",
	"/v1/agent/step_add",
	"/v1/agent/step_update",
	"/v1/agent/event_log",
	"/v1/agent/learning_add",
	"/v1/agent/learning_list",
	"/v1/agent/directive_list",
	"/v1/agent/recall",
	"/v1/agent/search",
	"/v1/operator/learning_decide",
	"/v1/operator/directive_ratify",
	"/v1/operator/directive_retire",
	"/v1/operator/project_settings_set",
}

// TestWirePathsAreCalls checks that each of the 19 literal paths is a call: a
// POST to it is answered by a handler, not 404. A renamed route fails here.
func TestWirePathsAreCalls(t *testing.T) {
	if len(wirePaths) != 19 {
		t.Fatalf("the literal list has %d paths, want 19", len(wirePaths))
	}
	h := api.Handler(core.Unimplemented{}, api.UnauthenticatedHeaderCaller, nil)
	for _, path := range wirePaths {
		rec := serve(h, http.MethodPost, path, `{}`, nil)
		if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
			t.Errorf("POST %s answered %d: the path is not a call", path, rec.Code)
		}
	}
}

// TestWireHeaderNamesCarryTheCaller checks the caller headers by their literal
// names. A request that sets each one reaches the service with that field.
func TestWireHeaderNamesCarryTheCaller(t *testing.T) {
	f := &fake{}
	h := api.Handler(f, api.UnauthenticatedHeaderCaller, nil)
	headers := map[string]string{
		"X-Cairn-Project": "acme-api",
		"X-Cairn-Agent":   "hicks",
		"X-Cairn-Session": "s-1",
		"X-Cairn-Client":  "claude-code",
		"X-Cairn-Role":    "operator",
	}
	serve(h, http.MethodPost, "/v1/agent/task_current", `{}`, headers)
	want := core.Caller{Project: "acme-api", Agent: "hicks", Session: "s-1", Client: "claude-code", Role: core.RoleOperator}
	if f.seen != want {
		t.Errorf("caller = %+v, want %+v", f.seen, want)
	}
}

// TestWireErrorBodyIsLiteral checks one error answer byte for byte: the body of
// a GET to a call is the literal JSON below, with the newline the encoder ends it
// with, and the status and content type are the wire's.
func TestWireErrorBodyIsLiteral(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.UnauthenticatedHeaderCaller, nil)
	rec := serve(h, http.MethodGet, "/v1/agent/task_start", "", nil)
	const want = `{"error":{"kind":"invalid","message":"a call is a POST with a JSON body"}}` + "\n"
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status %d, want 405", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type %q, want application/json", ct)
	}
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}
