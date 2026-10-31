package api_test

import (
	"net/http"
	"net/http/httptest"
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

// postWithType sends a POST of body to path with the Content-Type given. An empty
// contentType sends no Content-Type header at all.
func postWithType(h http.Handler, path, body, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	for name, value := range agentHeaders {
		req.Header.Set(name, value)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestBodyNeedsJSONContentType checks that a body with content is refused with 415
// and kind unsupported_media_type unless its Content-Type is application/json. A
// parameter such as charset is accepted, and so is any letter case of the type. The
// accepted requests reach the service, which answers 500 from core.Unimplemented.
func TestBodyNeedsJSONContentType(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.UnauthenticatedHeaderCaller, nil)
	const body = `{"title":"t","goal":"g"}`
	refused := []struct{ what, contentType string }{
		{"text/plain", "text/plain"},
		{"a form post", "application/x-www-form-urlencoded"},
		{"no Content-Type", ""},
	}
	for _, c := range refused {
		rec := postWithType(h, api.Prefix+api.RouteTaskStart, body, c.contentType)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%s: status %d, want 415", c.what, rec.Code)
			continue
		}
		if got := errorBody(t, rec); got.Kind != api.KindUnsupportedMediaType {
			t.Errorf("%s: kind %q, want unsupported_media_type", c.what, got.Kind)
		}
	}
	accepted := []string{"application/json", "application/json; charset=utf-8", "Application/JSON"}
	for _, contentType := range accepted {
		rec := postWithType(h, api.Prefix+api.RouteTaskStart, body, contentType)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("Content-Type %q: status %d, want 500 from the service", contentType, rec.Code)
		}
	}
}

// TestNoBodyNeedsNoContentType checks that a call with no body, or only JSON white
// space, is the zero request and needs no Content-Type: task_current takes none.
func TestNoBodyNeedsNoContentType(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.UnauthenticatedHeaderCaller, nil)
	for _, body := range []string{"", " \r\n"} {
		rec := postWithType(h, api.Prefix+api.RouteTaskCurrent, body, "")
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("body %q with no Content-Type: status %d, want 500 from the service", body, rec.Code)
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
