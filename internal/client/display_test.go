package client

import (
	"context"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestDisplayURLRemovesCredentials checks W5: the URL a message names carries no user
// name and no password, and keeps the rest of it.
func TestDisplayURLRemovesCredentials(t *testing.T) {
	cases := map[string]string{
		"https://keeper:secret@cairn.lan:8787": "https://cairn.lan:8787",
		"https://keeper@cairn.lan":             "https://cairn.lan",
		"http://127.0.0.1:8787/":               "http://127.0.0.1:8787/",
	}
	for in, want := range cases {
		if got := DisplayURL(in); got != want {
			t.Errorf("DisplayURL(%q) = %q, want %q", in, got, want)
		}
	}
	if got := DisplayURL("http://[bad"); strings.Contains(got, "bad") {
		t.Errorf("DisplayURL of a URL that does not parse = %q, want the fixed phrase", got)
	}
}

// TestCredentialsNeverReachAnError checks W5 on the two paths a URL can reach a
// message: a base that is refused when the client is built, and a call that fails in
// transport. Neither error may carry the user name or the password.
func TestCredentialsNeverReachAnError(t *testing.T) {
	if _, err := New("ftp://keeper:secret@cairn.lan", nil); err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "keeper") {
		t.Errorf("New refused the base with %v, want an error with no credentials", err)
	}

	// A closed port: the connection is refused, and net/http names the URL in its error.
	c, err := New("http://keeper:secret@127.0.0.1:1", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.TaskCurrent(core.WithCaller(context.Background(), core.Caller{Project: "acme-api", Role: core.RoleAgent}))
	if err == nil {
		t.Fatal("a call to a closed port succeeded")
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "keeper") {
		t.Errorf("the transport error carries the credentials: %v", err)
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Errorf("the transport error = %v, want the host still named", err)
	}
}
