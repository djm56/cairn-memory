package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/server"
)

// syncWriter is a log sink. The server writes to it from its own goroutine, and the test reads
// it, so each access takes the lock.
type syncWriter struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncWriter) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestKeyAddPrintsTheKeyOnceAndWarns checks the output of add: one key line on stdout, a warning
// on stderr that it is shown once, and a list that names the key by name and holds no key text.
func TestKeyAddPrintsTheKeyOnceAndWarns(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cairn.db")
	code, out, errs := runCmd(t, "key", "add", "--db", db, "--name", "ci", "--scope", "agent")
	if code != 0 {
		t.Fatalf("key add: exit %d, stderr %q", code, errs)
	}
	key := strings.TrimSpace(out)
	if !strings.HasPrefix(key, auth.KeyPrefix) || strings.Count(out, "\n") != 1 {
		t.Errorf("stdout %q is not one key line", out)
	}
	if !strings.Contains(errs, "shown once") {
		t.Errorf("stderr %q does not warn that the key is shown once", errs)
	}
	if strings.Contains(errs, key) {
		t.Errorf("stderr prints the key: %q", errs)
	}

	code, list, errs := runCmd(t, "key", "list", "--db", db)
	if code != 0 {
		t.Fatalf("key list: exit %d, stderr %q", code, errs)
	}
	for _, want := range []string{"NAME", "SCOPE", "ci", "agent"} {
		if !strings.Contains(list, want) {
			t.Errorf("list lacks %q:\n%s", want, list)
		}
	}
	if strings.Contains(list, key) || strings.Contains(list, auth.KeyPrefix) {
		t.Errorf("the list prints key text:\n%s", list)
	}
}

// TestKeyCommandUsageRefusals checks that each misuse is a usage error (exit 2) and names what
// is missing, and that help goes to stdout.
func TestKeyCommandUsageRefusals(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cairn.db")
	cases := []struct {
		what string
		args []string
		want string
	}{
		{"no action", []string{"key"}, "Usage: cairn key"},
		{"an unknown action", []string{"key", "nope"}, "unknown action"},
		{"add with no --db", []string{"key", "add", "--name", "ci", "--scope", "agent"}, "needs --db"},
		{"add with no --name", []string{"key", "add", "--db", db, "--scope", "agent"}, "needs --name"},
		{"add with no --scope", []string{"key", "add", "--db", db, "--name", "ci"}, "needs --scope"},
		{"add with a scope that is not one of three", []string{"key", "add", "--db", db, "--name", "ci", "--scope", "admin"}, "agent, operator or viewer"},
		{"add with a positional argument", []string{"key", "add", "--db", db, "--name", "ci", "--scope", "agent", "extra"}, "takes flags only"},
		{"list with no --db", []string{"key", "list"}, "needs --db"},
		{"revoke with no --name", []string{"key", "revoke", "--db", db}, "needs --name"},
	}
	for _, c := range cases {
		code, _, errs := runCmd(t, c.args...)
		if code != 2 || !strings.Contains(errs, c.want) {
			t.Errorf("%s: exit %d, stderr %q; want 2 and %q", c.what, code, errs, c.want)
		}
	}
	code, out, _ := runCmd(t, "key", "--help")
	if code != 0 || !strings.Contains(out, "cairn key add") {
		t.Errorf("key --help: exit %d, stdout %q", code, out)
	}
}

// TestKeyRevokeTwiceAndAnUnknownNameExitOne checks that a second revocation and a name with no
// key are exit 1, with the store's meaning in words.
func TestKeyRevokeTwiceAndAnUnknownNameExitOne(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cairn.db")
	if code, _, errs := runCmd(t, "key", "add", "--db", db, "--name", "ci", "--scope", "agent"); code != 0 {
		t.Fatalf("add: exit %d, stderr %q", code, errs)
	}
	if code, _, errs := runCmd(t, "key", "revoke", "--db", db, "--name", "ci"); code != 0 {
		t.Fatalf("first revoke: exit %d, stderr %q", code, errs)
	}
	for _, name := range []string{"ci", "nobody"} {
		code, _, errs := runCmd(t, "key", "revoke", "--db", db, "--name", name)
		if code != 1 || !strings.Contains(errs, "no active API key named "+name) {
			t.Errorf("revoke %s again: exit %d, stderr %q; want 1 and the not-found message", name, code, errs)
		}
	}
}

// TestKeyListOnAMissingDatabaseExitsOneAndCreatesNothing checks that list does not make an empty
// database from a mistyped path.
func TestKeyListOnAMissingDatabaseExitsOneAndCreatesNothing(t *testing.T) {
	db := filepath.Join(t.TempDir(), "absent.db")
	code, _, errs := runCmd(t, "key", "list", "--db", db)
	if code != 1 || !strings.Contains(errs, "no database at") {
		t.Errorf("list on a missing file: exit %d, stderr %q", code, errs)
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Errorf("list made the file: stat err %v", err)
	}
}

// waitForListener returns the base URL of the server whose log has its listening line, polling
// until it appears or the deadline passes.
func waitForListener(t *testing.T, logs *syncWriter) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(logs.String(), "\n") {
			if !strings.Contains(line, "msg=listening") {
				continue
			}
			for _, field := range strings.Fields(line) {
				if v, ok := strings.CutPrefix(field, "addr="); ok {
					return "http://" + strings.Trim(v, `"`)
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the server did not start listening; its log: %q", logs.String())
	return ""
}

// agentCall makes one agent call, task_current, with key as the bearer, and returns the status.
func agentCall(t *testing.T, c *http.Client, base, key string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/v1/agent/task_current", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Cairn-Project", "acme-api")
	req.Header.Set("X-Cairn-Agent", "main")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("call %s: %v", base, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// TestPrintedKeyAuthenticatesAgainstTheListenerAndStopsAfterRevoke is the end-to-end check of
// the key commands. The key add prints is accepted by a real cairn serve listener on
// 127.0.0.1:0, and after cairn key revoke the same key is refused by that running server.
func TestPrintedKeyAuthenticatesAgainstTheListenerAndStopsAfterRevoke(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cairn.db")
	code, out, errs := runCmd(t, "key", "add", "--db", db, "--name", "ci", "--scope", "agent")
	if code != 0 {
		t.Fatalf("key add: exit %d, stderr %q", code, errs)
	}
	key := strings.TrimSpace(out)

	logs := &syncWriter{}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	stopped := make(chan error, 1)
	go func() {
		stopped <- server.Run(ctx, server.Options{Addr: "127.0.0.1:0", DB: db, Log: logs})
	}()
	base := waitForListener(t, logs)
	c := &http.Client{Timeout: 10 * time.Second}

	if got := agentCall(t, c, base, key); got != http.StatusOK {
		t.Fatalf("the printed key: status %d, want 200", got)
	}
	if code, _, errs := runCmd(t, "key", "revoke", "--db", db, "--name", "ci"); code != 0 {
		t.Fatalf("revoke: exit %d, stderr %q", code, errs)
	}
	if got := agentCall(t, c, base, key); got != http.StatusUnauthorized {
		t.Errorf("the revoked key: status %d, want 401", got)
	}

	stop()
	select {
	case err := <-stopped:
		if err != nil {
			t.Errorf("server stopped with %v, want nil", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the server did not stop within 15s of its stop signal")
	}
	if strings.Contains(logs.String(), key) {
		t.Errorf("the server log holds the key")
	}
}

// keyFileFor writes client.env with mode under the test's home directory, and returns its path.
func keyFileFor(t *testing.T, mode os.FileMode, content string) string {
	t.Helper()
	dir := filepath.Join(os.Getenv("HOME"), ".config", "cairn")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "client.env")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestDoctorReportsWhereTheKeyCameFrom checks the key line of doctor in each case, and that no
// case prints the key. The key is the test's, so a print of it would show.
func TestDoctorReportsWhereTheKeyCameFrom(t *testing.T) {
	var seen []string
	_, url := newServer(t, &seen)
	dir := t.TempDir()
	useDir(t, dir)
	writeFile(t, dir, ".cairn.toml", "project = \"acme-api\"\nserver = \""+url+"\"\n")
	envKey := os.Getenv("CAIRN_API_KEY")
	if envKey == "" {
		t.Fatal("the test key is empty: a search for it would match every output")
	}
	// noKey fails the case when the output holds the key. It runs after every invocation, so
	// each case's output is checked, including the cases where the key is sent and the ones
	// where it is refused (step 8 review W1).
	noKey := func(what, out string) {
		t.Helper()
		if strings.Contains(out, envKey) {
			t.Errorf("%s: doctor printed the key:\n%s", what, out)
		}
	}

	code, out, _ := runCmd(t, "doctor")
	if code != 0 || !strings.Contains(out, "key      ok    found (env)") {
		t.Errorf("from the environment: exit %d, output:\n%s", code, out)
	}
	noKey("from the environment", out)

	t.Setenv("CAIRN_API_KEY", "")
	keyFileFor(t, 0o600, "CAIRN_API_KEY="+envKey+"\n")
	code, out, _ = runCmd(t, "doctor")
	if code != 0 || !strings.Contains(out, "key      ok    found (client.env)") {
		t.Errorf("from client.env: exit %d, output:\n%s", code, out)
	}
	noKey("from client.env, sent", out)

	keyFileFor(t, 0o644, "CAIRN_API_KEY="+envKey+"\n")
	code, out, _ = runCmd(t, "doctor")
	if code != 1 || !strings.Contains(out, "chmod 600") {
		t.Errorf("client.env open to others: exit %d, output:\n%s", code, out)
	}
	noKey("client.env open to others", out)

	keyFileFor(t, 0o600, "# no key here\n")
	code, out, _ = runCmd(t, "doctor")
	if code != 1 || !strings.Contains(out, "key      fail  missing:") || !strings.Contains(out, "reach    skip") {
		t.Errorf("no key: exit %d, output:\n%s", code, out)
	}
	noKey("no key", out)
}

// TestDoctorSaysTheServerRefusedTheKey checks that a server which refuses the key fails the reach
// check with the refusal, and that the output holds no key.
func TestDoctorSaysTheServerRefusedTheKey(t *testing.T) {
	refuse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"kind":"unauthenticated","message":"unauthenticated"}}`))
	}))
	t.Cleanup(refuse.Close)
	dir := t.TempDir()
	useDir(t, dir)
	writeFile(t, dir, ".cairn.toml", "project = \"acme-api\"\nserver = \""+refuse.URL+"\"\n")
	code, out, _ := runCmd(t, "doctor")
	if code != 1 || !strings.Contains(out, "refused the API key") {
		t.Errorf("a refusing server: exit %d, output:\n%s", code, out)
	}
	if strings.Contains(out, os.Getenv("CAIRN_API_KEY")) {
		t.Errorf("doctor printed the key")
	}
}

// TestMCPRefusesToStartWithNoKey checks item 6 of the key brief: with a server set and no key in
// either place, cairn mcp refuses to start, names both places, and writes nothing to stdout.
func TestMCPRefusesToStartWithNoKey(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", "http://127.0.0.1:1")
	t.Setenv("CAIRN_API_KEY", "")
	var stdout, stderr bytes.Buffer
	code := serveMCP(nil, io.NopCloser(strings.NewReader("")), &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit %d, want 1 (stderr %q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "CAIRN_API_KEY") || !strings.Contains(stderr.String(), "client.env") {
		t.Errorf("stderr %q does not name both sources", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout %q, want nothing: it is the protocol's", stdout.String())
	}
}
