package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

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

// nestArrays returns a body of depth arrays, each opened and closed: [[...]].
func nestArrays(depth int) string {
	return strings.Repeat("[", depth) + strings.Repeat("]", depth)
}

// refusedForDepth reports whether an answer is the depth refusal, and not some
// other 400 the decoder gave.
func refusedForDepth(rec *httptest.ResponseRecorder) bool {
	return rec.Code == http.StatusBadRequest && strings.Contains(rec.Body.String(), "nests deeper")
}

// TestDepthIsBounded checks that a body nested one level past maxNestingDepth
// (strict.go) is refused with 400 and kind invalid, at the top level, inside a
// string field of a request, and inside free-form event data. The unclosed
// top-level body is the one that would drive an unbounded walk through the whole
// of its brackets.
func TestDepthIsBounded(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.UnauthenticatedHeaderCaller, nil)
	const over = 10001
	cases := []struct {
		what string
		path string
		body string
	}{
		{"a top-level body, unclosed", api.RouteTaskStart, strings.Repeat("[", over)},
		{"a top-level body, closed", api.RouteTaskStart, nestArrays(over)},
		{"a field of a request", api.RouteTaskStart, `{"title":` + nestArrays(over) + `,"goal":"g"}`},
		// The object is level 1, so its data holds one level less than over.
		{"event data", api.RouteEventLog, `{"task_id":"task-20261031-01","kind":"note","text":"t","data":` + nestArrays(over-1) + `}`},
	}
	for _, c := range cases {
		rec := serve(h, http.MethodPost, api.Prefix+c.path, c.body, agentHeaders)
		if !refusedForDepth(rec) {
			t.Errorf("%s: status %d, want the depth refusal 400: %.200s", c.what, rec.Code, rec.Body.String())
			continue
		}
		if got := errorBody(t, rec); got.Kind != core.KindInvalid {
			t.Errorf("%s: kind %q, want invalid", c.what, got.Kind)
		}
	}
}

// TestDepthAtTheLimitIsNotRefused checks the other side of the bound: a body
// nested exactly maxNestingDepth deep, and one just under it, is not refused for
// depth. Event data is free-form, so a body in it reaches the service, which
// answers its own internal failure (500). At the top level the body is the wrong
// shape for the request, so the decoder refuses it, but not for depth.
func TestDepthAtTheLimitIsNotRefused(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.UnauthenticatedHeaderCaller, nil)
	for _, depth := range []int{10000, 9999} {
		// The object is one of the depth levels, so its data holds depth-1 arrays.
		body := `{"task_id":"task-20261031-01","kind":"note","text":"t","data":` + nestArrays(depth-1) + `}`
		rec := serve(h, http.MethodPost, api.Prefix+api.RouteEventLog, body, agentHeaders)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("event data at %d levels: status %d, want 500 from the service: %.200s", depth, rec.Code, rec.Body.String())
		}
	}
	for _, depth := range []int{10000, 9999} {
		rec := serve(h, http.MethodPost, api.Prefix+api.RouteTaskStart, nestArrays(depth), agentHeaders)
		if refusedForDepth(rec) {
			t.Errorf("top level at %d levels: refused for depth: %s", depth, rec.Body.String())
		}
	}
}

// TestDepthLimitMatchesDecoder checks that maxNestingDepth is the limit
// encoding/json applies: a body nested to it is valid JSON, and one level deeper
// is not. If a later Go release changes its limit, this fails, and maxNestingDepth
// must follow.
func TestDepthLimitMatchesDecoder(t *testing.T) {
	if !json.Valid([]byte(nestArrays(10000))) {
		t.Error("a body nested 10000 deep is not valid JSON: the decoder's limit is lower than maxNestingDepth")
	}
	if json.Valid([]byte(nestArrays(10001))) {
		t.Error("a body nested 10001 deep is valid JSON: the decoder's limit is higher than maxNestingDepth")
	}
}

// TestEscapedDuplicateKeyIsRefused checks that a key written with an escape is
// the same key for the repeat rule: "title" decodes to "title", so the
// second title is refused with 400.
func TestEscapedDuplicateKeyIsRefused(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.UnauthenticatedHeaderCaller, nil)
	rec := serve(h, http.MethodPost, api.Prefix+api.RouteTaskStart, `{"title":"a","title":"b","goal":"g"}`, agentHeaders)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	got := errorBody(t, rec)
	if got.Kind != core.KindInvalid || !strings.Contains(got.Message, "repeats the key") {
		t.Errorf("body = %+v, want invalid and a message that says repeats the key", got)
	}
}

// TestOperatorRouteIsStrict checks one operator call through the strict path: a
// mixed-case key is refused with 400, and the same call with exact keys reaches
// the service.
func TestOperatorRouteIsStrict(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.UnauthenticatedHeaderCaller, nil)
	operator := map[string]string{api.HeaderProject: "acme-api", api.HeaderAgent: "main", api.HeaderRole: string(core.RoleOperator)}
	path := api.Prefix + api.RouteDirectiveRatify

	rec := serve(h, http.MethodPost, path, `{"Title":"t","scope":"project","project":"acme-api"}`, operator)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mixed-case key: status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if got := errorBody(t, rec); got.Kind != core.KindInvalid || !strings.Contains(got.Message, "case-sensitive") {
		t.Errorf("mixed-case key: body = %+v, want invalid and case-sensitive", got)
	}

	rec = serve(h, http.MethodPost, path, `{"title":"t","scope":"project","project":"acme-api"}`, operator)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("exact keys: status %d, want 500 from the service: %s", rec.Code, rec.Body.String())
	}
}

// TestBodyChecksRunInOrder checks the order of the body checks: the size is read
// first, so an oversized body is 413 whatever its type, and the type is checked
// before the keys, so a body with a key fault and no JSON type is 415.
func TestBodyChecksRunInOrder(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.UnauthenticatedHeaderCaller, nil)

	big := `{"title":"` + strings.Repeat("x", api.MaxBodyBytes) + `"}`
	rec := postWithType(h, api.Prefix+api.RouteTaskStart, big, "text/plain")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized text/plain body: status %d, want 413 before 415", rec.Code)
	} else if got := errorBody(t, rec); got.Kind != core.KindInvalid {
		t.Errorf("oversized body: kind %q, want invalid", got.Kind)
	}

	rec = postWithType(h, api.Prefix+api.RouteTaskStart, `{"Title":"t","goal":"g"}`, "text/plain")
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain body with a key fault: status %d, want 415 before 400", rec.Code)
	}
}

// TestRefusedKeyIsTruncated checks that a key echoed in a refusal is cut to
// maxEchoedKey bytes and marked with "…", so a long key does not come back in full,
// and that a cut falls on a character boundary, so the message is still UTF-8.
func TestRefusedKeyIsTruncated(t *testing.T) {
	h := api.Handler(core.Unimplemented{}, api.UnauthenticatedHeaderCaller, nil)

	long := strings.Repeat("k", 5000)
	rec := serve(h, http.MethodPost, api.Prefix+api.RouteTaskStart, `{"`+long+`":"a","`+long+`":"b"}`, agentHeaders)
	got := errorBody(t, rec)
	if rec.Code != http.StatusBadRequest || !strings.Contains(got.Message, "repeats the key") {
		t.Fatalf("long key: status %d, body = %+v, want 400 and a repeated-key message", rec.Code, got)
	}
	if !strings.Contains(got.Message, strings.Repeat("k", 64)+"…") || strings.Contains(got.Message, strings.Repeat("k", 65)) {
		t.Errorf("long key: message %q is not cut to 64 bytes and marked", got.Message)
	}
	if len(got.Message) > 200 {
		t.Errorf("long key: message is %d bytes, want the key cut", len(got.Message))
	}

	// Each € is three bytes, so the 64-byte cut falls inside the 22nd.
	multi := strings.Repeat("€", 30)
	rec = serve(h, http.MethodPost, api.Prefix+api.RouteTaskStart, `{"`+multi+`":"a","`+multi+`":"b"}`, agentHeaders)
	got = errorBody(t, rec)
	if !utf8.ValidString(got.Message) {
		t.Errorf("multi-byte key: message %q is not valid UTF-8", got.Message)
	}
	if !strings.Contains(got.Message, strings.Repeat("€", 21)+"…") || strings.Contains(got.Message, strings.Repeat("€", 22)) {
		t.Errorf("multi-byte key: message %q is not cut back to 21 characters", got.Message)
	}
}

// TestRequestTypesHaveNoUnmarshaler keeps the assumption checkKeys makes (its
// docblock): no request type the handlers decode has an UnmarshalJSON, so the walk
// reads each one as a plain struct. It walks the request type of every Service
// call, and every type those reach. json.RawMessage is exempt: it is free-form,
// and the walk reads event data that way.
func TestRequestTypesHaveNoUnmarshaler(t *testing.T) {
	unmarshaler := reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	rawMessage := reflect.TypeOf(json.RawMessage(nil))
	svc := reflect.TypeOf((*core.Service)(nil)).Elem()
	seen := map[reflect.Type]bool{}
	var visit func(rt reflect.Type)
	visit = func(rt reflect.Type) {
		if seen[rt] || rt == rawMessage {
			return
		}
		seen[rt] = true
		switch rt.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			visit(rt.Elem())
			return
		case reflect.Struct:
		default:
			return
		}
		if reflect.PointerTo(rt).Implements(unmarshaler) {
			t.Errorf("%s has an UnmarshalJSON: checkKeys would walk it as a struct", rt)
		}
		for i := range rt.NumField() {
			visit(rt.Field(i).Type)
		}
	}
	calls := 0
	for i := range svc.NumMethod() {
		m := svc.Method(i)
		if m.Type.NumIn() < 2 {
			continue
		}
		calls++
		visit(m.Type.In(1))
	}
	if calls == 0 {
		t.Fatal("found no request types: the check would pass for nothing")
	}
}
