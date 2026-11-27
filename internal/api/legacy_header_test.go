package api

import (
	"net/http/httptest"
	"testing"
)

// TestCallerHeaderReadsTheOldName checks that a caller header sent under its name from before
// the rename to Cairnbook, X-Cairn-*, is read when the request has no X-Cairnbook-* one, and
// that the new name wins when both are sent, even when the new one is empty.
func TestCallerHeaderReadsTheOldName(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("X-Cairn-Project", "old-project")
	if got := callerHeader(r, HeaderProject); got != "old-project" {
		t.Errorf("old header alone: got %q, want old-project", got)
	}
	r.Header.Set(HeaderProject, "new-project")
	if got := callerHeader(r, HeaderProject); got != "new-project" {
		t.Errorf("both headers: got %q, want new-project", got)
	}
	r.Header.Set(HeaderProject, "")
	if got := callerHeader(r, HeaderProject); got != "" {
		t.Errorf("new header empty: got %q, want empty", got)
	}
	r = httptest.NewRequest("POST", "/", nil)
	r.Header.Set("X-Cairn-Role", "operator")
	c, _ := UnauthenticatedHeaderCaller(r)
	if c.Role != "operator" {
		t.Errorf("old role header: got role %q, want operator", c.Role)
	}
}
