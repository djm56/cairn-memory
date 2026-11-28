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

// TestEmptyNewCallerHeaderWinsOverTheOldName checks that X-Cairnbook-Agent, -Session and -Client sent
// empty win over the X-Cairn-* header of the same field beside them, as the project header does in
// TestCallerHeaderReadsTheOldName.
func TestEmptyNewCallerHeaderWinsOverTheOldName(t *testing.T) {
	cases := []struct{ name, old string }{
		{HeaderAgent, "X-Cairn-Agent"},
		{HeaderSession, "X-Cairn-Session"},
		{HeaderClient, "X-Cairn-Client"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("POST", "/", nil)
		r.Header.Set(c.old, "old-value")
		r.Header.Set(c.name, "")
		if got := callerHeader(r, c.name); got != "" {
			t.Errorf("%s empty beside %s: got %q, want empty", c.name, c.old, got)
		}
	}
}
