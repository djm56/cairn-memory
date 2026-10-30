package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// fake is a Service whose TaskStart fails with err and whose TaskGet answers
// detail. Its TaskCurrent records the caller it was given, in seen.
type fake struct {
	core.Unimplemented
	err    error
	detail *core.TaskDetail
	seen   core.Caller
}

func (f *fake) TaskStart(context.Context, core.TaskStartRequest) (*core.TaskStartResult, error) {
	return nil, f.err
}

func (f *fake) TaskGet(context.Context, core.TaskGetRequest) (*core.TaskDetail, error) {
	return f.detail, f.err
}

func (f *fake) TaskCurrent(ctx context.Context) (*core.TaskCurrent, error) {
	f.seen, _ = core.CallerFrom(ctx)
	return &core.TaskCurrent{}, nil
}

// serve sends one request to h, in process, and returns what it answered.
func serve(h http.Handler, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// agentHeaders names a project and an agent, as a call from an agent does.
var agentHeaders = map[string]string{api.HeaderProject: "acme-api", api.HeaderAgent: "main"}

// errorBody reads the error body of an answer.
func errorBody(t *testing.T, rec *httptest.ResponseRecorder) *core.Error {
	t.Helper()
	var body api.ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error == nil {
		t.Fatalf("answer %q is not an error body: %v", rec.Body.String(), err)
	}
	return body.Error
}

// openLocal returns a Service over a new database, for the calls that need one.
func openLocal(t *testing.T) *core.Local {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cairn.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return core.NewLocal(st)
}

// TestErrorKindsKeepTheirStatusAndBody checks that each core kind is answered
// with its status and carries its kind, message and missing list in the body.
func TestErrorKindsKeepTheirStatusAndBody(t *testing.T) {
	cases := []struct {
		err    *core.Error
		status int
	}{
		{&core.Error{Kind: core.KindInvalid, Message: "fix the request"}, http.StatusBadRequest},
		{&core.Error{Kind: core.KindNotFound, Message: "task task-1 not found"}, http.StatusNotFound},
		{&core.Error{Kind: core.KindRefused, Message: "close policy not met", Missing: []string{"summary", "lessons"}}, http.StatusConflict},
		{&core.Error{Kind: core.KindOperatorOnly, Message: "an operator call"}, http.StatusForbidden},
	}
	for _, c := range cases {
		h := api.Handler(&fake{err: c.err}, api.HeaderCaller)
		rec := serve(h, http.MethodPost, api.Prefix+api.RouteTaskStart, `{"title":"t","goal":"g"}`, agentHeaders)
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d", c.err.Kind, rec.Code, c.status)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s: content type %q", c.err.Kind, ct)
		}
		got := errorBody(t, rec)
		if got.Kind != c.err.Kind || got.Message != c.err.Message || !slices.Equal(got.Missing, c.err.Missing) {
			t.Errorf("%s: body = %+v, want %+v", c.err.Kind, got, c.err)
		}
	}
}

// TestInternalFailureHidesItsCause checks that an error which is not a
// core.Error is answered 500 with the kind internal and a fixed message, and
// that its text does not reach the caller.
func TestInternalFailureHidesItsCause(t *testing.T) {
	h := api.Handler(&fake{err: errors.New("disk on fire")}, api.HeaderCaller)
	rec := serve(h, http.MethodPost, api.Prefix+api.RouteTaskStart, `{"title":"t","goal":"g"}`, agentHeaders)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
	got := errorBody(t, rec)
	if got.Kind != api.KindInternal || got.Message != "internal error" {
		t.Errorf("body = %+v, want kind internal and message internal error", got)
	}
	if strings.Contains(rec.Body.String(), "disk on fire") {
		t.Errorf("the cause reached the caller: %s", rec.Body.String())
	}
}

// TestCallsAreOnlyPost checks that a call answers a method other than POST with
// 405, names POST in Allow, and carries the error body.
func TestCallsAreOnlyPost(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.HeaderCaller)
	rec := serve(h, http.MethodGet, api.Prefix+api.RouteTaskStart, "", agentHeaders)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); allow != http.MethodPost {
		t.Errorf("Allow = %q, want POST", allow)
	}
	if got := errorBody(t, rec); got.Kind != core.KindInvalid {
		t.Errorf("GET kind = %q, want invalid", got.Kind)
	}
}

// TestMalformedRequestsAreInvalid checks that a body which is not the call's
// request is refused before the service is reached. Core.Unimplemented answers
// every call with an internal failure, so a 400 or 413 shows the handler stopped.
func TestMalformedRequestsAreInvalid(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.HeaderCaller)
	huge := `{"title":"` + strings.Repeat("x", api.MaxBodyBytes) + `","goal":"g"}`
	cases := []struct {
		what   string
		body   string
		status int
		text   string // a word the message must contain, or ""
	}{
		{"truncated JSON", `{"title":`, http.StatusBadRequest, ""},
		{"an array, not an object", `[]`, http.StatusBadRequest, ""},
		{"an unknown field", `{"title":"t","goal":"g","limt":1}`, http.StatusBadRequest, "limt"},
		{"two JSON values", `{}{}`, http.StatusBadRequest, ""},
		{"a string for a number", `{"title":"t","goal":"g","priority":5}`, http.StatusBadRequest, ""},
		{"a body over the cap", huge, http.StatusRequestEntityTooLarge, ""},
	}
	for _, c := range cases {
		rec := serve(h, http.MethodPost, api.Prefix+api.RouteTaskStart, c.body, agentHeaders)
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d", c.what, rec.Code, c.status)
			continue
		}
		got := errorBody(t, rec)
		if got.Kind != core.KindInvalid {
			t.Errorf("%s: kind %q, want invalid", c.what, got.Kind)
		}
		if c.text != "" && !strings.Contains(got.Message, c.text) {
			t.Errorf("%s: message %q does not name %q", c.what, got.Message, c.text)
		}
	}
}

// TestUnknownPathIsNotACall checks that a path which is no call is answered 404
// with the kind invalid, and names where the calls are. A missing trailing
// slash and a missing prefix both count.
func TestUnknownPathIsNotACall(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.HeaderCaller)
	for _, path := range []string{
		api.Prefix + "/agent/nope",
		api.Prefix + api.RouteTaskStart + "/",
		api.RouteTaskStart,
	} {
		rec := serve(h, http.MethodPost, path, `{}`, agentHeaders)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, rec.Code)
			continue
		}
		got := errorBody(t, rec)
		if got.Kind != core.KindInvalid || !strings.Contains(got.Message, "/v1/agent") {
			t.Errorf("%s: body = %+v", path, got)
		}
	}
}

// TestHeadersCarryTheCaller checks that the Cairn headers become the caller of
// the call, and that a call with no role header is an agent.
func TestHeadersCarryTheCaller(t *testing.T) {
	f := &fake{}
	h := api.Handler(f, api.HeaderCaller)
	headers := map[string]string{
		api.HeaderProject: "acme-api", api.HeaderAgent: "hicks",
		api.HeaderSession: "s-1", api.HeaderClient: "claude-code", api.HeaderRole: "operator",
	}
	if rec := serve(h, http.MethodPost, api.Prefix+api.RouteTaskCurrent, "", headers); rec.Code != http.StatusOK {
		t.Fatalf("task_current status %d: %s", rec.Code, rec.Body.String())
	}
	want := core.Caller{Project: "acme-api", Agent: "hicks", Session: "s-1", Client: "claude-code", Role: core.RoleOperator}
	if f.seen != want {
		t.Errorf("caller = %+v, want %+v", f.seen, want)
	}

	delete(headers, api.HeaderRole)
	serve(h, http.MethodPost, api.Prefix+api.RouteTaskCurrent, "{}", headers)
	if f.seen.Role != core.RoleAgent {
		t.Errorf("role with no role header = %q, want agent", f.seen.Role)
	}
}

// TestEmptyBodyIsTheZeroRequest checks that a call with no body, or an empty
// object, reaches the service as the zero request.
func TestEmptyBodyIsTheZeroRequest(t *testing.T) {
	f := &fake{}
	h := api.Handler(f, api.HeaderCaller)
	for _, body := range []string{"", "{}"} {
		if rec := serve(h, http.MethodPost, api.Prefix+api.RouteTaskCurrent, body, agentHeaders); rec.Code != http.StatusOK {
			t.Errorf("body %q: status %d", body, rec.Code)
		}
	}
}

// TestMissingProjectNamesInit checks that a call with no project is invalid,
// and that its message names the fix (CONTRACT §1.1). The core decides this, so
// the test runs over a real Local.
func TestMissingProjectNamesInit(t *testing.T) {
	h := api.Handler(openLocal(t), api.HeaderCaller)
	rec := serve(h, http.MethodPost, api.Prefix+api.RouteTaskCurrent, "{}", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", rec.Code)
	}
	if got := errorBody(t, rec); got.Kind != core.KindInvalid || !strings.Contains(got.Message, "cairn init") {
		t.Errorf("body = %+v, want invalid and a message that names cairn init", got)
	}
}

// TestOperatorCallNeedsTheOperatorRole checks the role at the wire: an agent
// call on an operator route is operator_only, and the same call with the
// operator role reaches the service and finds no such learning.
func TestOperatorCallNeedsTheOperatorRole(t *testing.T) {
	h := api.Handler(openLocal(t), api.HeaderCaller)
	path := api.Prefix + api.RouteLearningDecide
	body := `{"id":999,"status":"accepted"}`

	rec := serve(h, http.MethodPost, path, body, agentHeaders)
	if rec.Code != http.StatusForbidden || errorBody(t, rec).Kind != core.KindOperatorOnly {
		t.Errorf("agent on an operator route: status %d, body %s", rec.Code, rec.Body.String())
	}

	operator := map[string]string{api.HeaderRole: "operator", api.HeaderAgent: "operator"}
	rec = serve(h, http.MethodPost, path, body, operator)
	if rec.Code != http.StatusNotFound || errorBody(t, rec).Kind != core.KindNotFound {
		t.Errorf("operator on an operator route: status %d, body %s", rec.Code, rec.Body.String())
	}
}

// TestEmptyEventsPageIsWritten checks the wire form of the events section (CONTRACT
// §2): a read of events writes "events": [] when the page is empty, and a read
// that did not ask for events writes no events key.
func TestEmptyEventsPageIsWritten(t *testing.T) {
	f := &fake{detail: &core.TaskDetail{Task: core.Task{ID: "task-20261030-01"}, Events: []core.Event{}}}
	h := api.Handler(f, api.HeaderCaller)
	body := `{"id":"task-20261030-01","section":"events"}`
	rec := serve(h, http.MethodPost, api.Prefix+api.RouteTaskGet, body, agentHeaders)
	if !strings.Contains(rec.Body.String(), `"events":[]`) {
		t.Errorf("empty events page answered %s, want \"events\":[]", rec.Body.String())
	}

	f.detail = &core.TaskDetail{Task: core.Task{ID: "task-20261030-01"}}
	rec = serve(h, http.MethodPost, api.Prefix+api.RouteTaskGet, `{"id":"task-20261030-01","section":"brief"}`, agentHeaders)
	if strings.Contains(rec.Body.String(), `"events"`) {
		t.Errorf("brief answered %s, want no events key", rec.Body.String())
	}
}

// TestHandlerNeedsACallerFunc checks that Handler refuses to build with no
// caller resolver, since a default identity would be a silent one.
func TestHandlerNeedsACallerFunc(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Handler with a nil CallerFunc did not panic")
		}
	}()
	api.Handler(core.Unimplemented{}, nil)
}
