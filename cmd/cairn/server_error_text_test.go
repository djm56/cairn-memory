package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serverErrorMessage is an error message a server might send. It carries an escape
// sequence, a bell, a carriage return and a bidi override, so every print path has
// something to remove. Its one-line form is serverErrorLine.
const (
	serverErrorMessage = "boom\x1b[2J one\u202e two\u200b\a\rthree"
	serverErrorLine    = "boom one two three"
)

// errorServer answers every call with the core error kind and message, as a Cairn server
// does for a failure. It is the server a test points a command at.
func errorServer(t *testing.T, kind, message string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"error": map[string]any{"kind": kind, "message": message}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestServerErrorTextIsOneLineOnEveryPath is step 12 review W3. A server's error message
// is printed by status, export, the hook's stderr and doctor's detail, and each of them
// prints it as one printable line: no escape sequence, no control character and no format
// character reaches the terminal or the model.
func TestServerErrorTextIsOneLineOnEveryPath(t *testing.T) {
	invalid := errorServer(t, "invalid", serverErrorMessage)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_PROJECT", "acme-api")

	t.Run("status", func(t *testing.T) {
		t.Setenv("CAIRN_SERVER", invalid)
		code, stdout, stderr := runCmd(t, "status")
		if code != 1 || stdout != "" {
			t.Errorf("exit %d, stdout %q: want 1 and nothing on stdout", code, stdout)
		}
		if want := "cairn status: " + serverErrorLine + "\n"; stderr != want {
			t.Errorf("stderr %q, want %q", stderr, want)
		}
	})

	t.Run("export", func(t *testing.T) {
		t.Setenv("CAIRN_SERVER", invalid)
		code, stdout, stderr := runCmd(t, "export", "task", "acme-1")
		if code != 1 || stdout != "" {
			t.Errorf("exit %d, stdout %q: want 1 and nothing on stdout", code, stdout)
		}
		if want := "cairn export task: " + serverErrorLine + "\n"; stderr != want {
			t.Errorf("stderr %q, want %q", stderr, want)
		}
	})

	t.Run("hook", func(t *testing.T) {
		t.Setenv("CAIRN_SERVER", invalid)
		code, stdout, stderr := runCmd(t, "hook", "session-start")
		if code != 0 || stdout != "" {
			t.Errorf("exit %d, stdout %q: want 0 and nothing on stdout", code, stdout)
		}
		if want := "cairn hook session-start: no context added: " + serverErrorLine + "\n"; stderr != want {
			t.Errorf("stderr %q, want %q", stderr, want)
		}
	})

	t.Run("doctor", func(t *testing.T) {
		// A refused answer is a fault for doctor; an invalid one would pass as a Cairn server.
		t.Setenv("CAIRN_SERVER", errorServer(t, "refused", serverErrorMessage))
		code, stdout, stderr := runCmd(t, "doctor")
		if code != 1 {
			t.Errorf("exit %d, want 1 (stdout %q, stderr %q)", code, stdout, stderr)
		}
		if !strings.Contains(stdout, "reach    fail  "+serverErrorLine+"\n") {
			t.Errorf("doctor lacks the one-line detail:\n%s", stdout)
		}
		if strings.ContainsAny(stdout+stderr, "\x1b\a\r\u202e\u200b") {
			t.Errorf("doctor prints a control or format character: stdout %q, stderr %q", stdout, stderr)
		}
	})
}
