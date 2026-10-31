package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/core"
)

// TestMixedCaseKeyIsRefused checks that a key written in a case other than the
// field's is refused with 400 and kind invalid, at the top level and nested. The
// service is core.Unimplemented, which answers 500, so a 400 shows the handler
// stopped before it.
func TestMixedCaseKeyIsRefused(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.UnauthenticatedHeaderCaller, nil)
	cases := []struct {
		what string
		body string
		text string
	}{
		{"a top-level key", `{"Title":"x","goal":"g"}`, `"title"`},
		{"a key in an upper case", `{"title":"x","GOAL":"g"}`, `"goal"`},
		{"a nested key", `{"title":"t","goal":"g","steps":[{"Title":"s"}]}`, `"title"`},
	}
	for _, c := range cases {
		rec := serve(h, http.MethodPost, api.Prefix+api.RouteTaskStart, c.body, agentHeaders)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", c.what, rec.Code)
			continue
		}
		got := errorBody(t, rec)
		if got.Kind != core.KindInvalid || !strings.Contains(got.Message, "case-sensitive") || !strings.Contains(got.Message, c.text) {
			t.Errorf("%s: body = %+v, want invalid, case-sensitive, naming %s", c.what, got, c.text)
		}
	}
}

// TestRepeatedKeyIsRefused checks that a key given twice is refused with 400 and
// kind invalid, at the top level, nested in a list, and inside free-form event
// data, which has no field names but still may not repeat a key.
func TestRepeatedKeyIsRefused(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.UnauthenticatedHeaderCaller, nil)
	cases := []struct {
		what string
		path string
		body string
	}{
		{"a top-level key", api.RouteTaskStart, `{"title":"a","title":"b","goal":"g"}`},
		{"a nested key", api.RouteTaskStart, `{"title":"t","goal":"g","steps":[{"title":"a","title":"b"}]}`},
		{"a key in event data", api.RouteEventLog, `{"task_id":"task-20261031-01","kind":"note","text":"t","data":{"a":1,"a":2}}`},
	}
	for _, c := range cases {
		rec := serve(h, http.MethodPost, api.Prefix+c.path, c.body, agentHeaders)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", c.what, rec.Code)
			continue
		}
		got := errorBody(t, rec)
		if got.Kind != core.KindInvalid || !strings.Contains(got.Message, "repeats the key") {
			t.Errorf("%s: body = %+v, want invalid and a message that says repeats the key", c.what, got)
		}
	}
}

// TestExactKeysReachTheService is the control for the two tests above: a request
// whose keys are written exactly as the fields are, nested or in event data, is
// not refused by the wire form. The service answers its own internal failure.
func TestExactKeysReachTheService(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.UnauthenticatedHeaderCaller, nil)
	cases := []struct {
		what string
		path string
		body string
	}{
		{"exact nested keys", api.RouteTaskStart, `{"title":"t","goal":"g","steps":[{"title":"a"}]}`},
		{"free-form data with any key case", api.RouteEventLog, `{"task_id":"task-20261031-01","kind":"note","text":"t","data":{"Verdict":"x"}}`},
	}
	for _, c := range cases {
		rec := serve(h, http.MethodPost, api.Prefix+c.path, c.body, agentHeaders)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status %d, want 500 from the service: %s", c.what, rec.Code, rec.Body.String())
		}
	}
}
