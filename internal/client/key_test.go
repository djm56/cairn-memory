package client_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/client"
)

// keyEnv points the key lookup at a fresh home directory, with CAIRN_API_KEY set to env
// (or unset when env is ""). It returns the home, so a test can write client.env in it.
// Every key here comes from auth.NewKey, so no fixture holds a key-shaped literal.
func keyEnv(t *testing.T, env string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(client.EnvKey, env)
	return home
}

// writeKeyFile writes client.env under home with the given mode and content, and returns its
// path. The directories are made as the lookup expects them.
func writeKeyFile(t *testing.T, home string, mode os.FileMode, content string) string {
	t.Helper()
	dir := filepath.Join(home, ".config", "cairn")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "client.env")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	// WriteFile applies the umask, so set the mode the test asked for.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

// newKey returns a fresh key, failing the test when none can be made.
func newKey(t *testing.T) string {
	t.Helper()
	k, err := auth.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	return k
}

// TestEnvWinsOverTheFile checks the order of the two sources: CAIRN_API_KEY is used when it is
// set, even when client.env holds a different key. (can-fail: swapping the order fails this.)
func TestEnvWinsOverTheFile(t *testing.T) {
	fromEnv, fromFile := newKey(t), newKey(t)
	home := keyEnv(t, fromEnv)
	writeKeyFile(t, home, 0o600, "CAIRN_API_KEY="+fromFile+"\n")

	k, err := client.LoadKey()
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}
	if k.Value != fromEnv || k.Source != client.SourceEnv {
		t.Errorf("LoadKey = %v from %s, want the environment's key from env", k, k.Source)
	}
}

// TestTheFileIsUsedWhenTheEnvIsUnsetOrBlank checks that the file answers when the environment
// names no key: unset, or only white space.
func TestTheFileIsUsedWhenTheEnvIsUnsetOrBlank(t *testing.T) {
	for _, env := range []string{"", "   "} {
		fromFile := newKey(t)
		home := keyEnv(t, env)
		writeKeyFile(t, home, 0o600, "# the operator's key\nCAIRN_API_KEY=\""+fromFile+"\"\n")

		k, err := client.LoadKey()
		if err != nil {
			t.Fatalf("env %q: LoadKey: %v", env, err)
		}
		if k.Value != fromFile || k.Source != client.SourceFile {
			t.Errorf("env %q: LoadKey = %v from %s, want the file's key from client.env", env, k, k.Source)
		}
	}
}

// TestAFileOpenToOthersIsRefused checks the mode rule: a client.env any other user can read, or
// write, is not used. The refusal names the file and chmod 600, and it does not print the key.
// (can-fail: removing the mode check fails this.)
func TestAFileOpenToOthersIsRefused(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o602} {
		key := newKey(t)
		home := keyEnv(t, "")
		path := writeKeyFile(t, home, mode, "CAIRN_API_KEY="+key+"\n")

		k, err := client.LoadKey()
		if err == nil {
			t.Errorf("mode %o: LoadKey accepted the file, and returned %v", mode, k)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, path) || !strings.Contains(msg, "chmod 600") {
			t.Errorf("mode %o: the refusal %q does not name the file and chmod 600", mode, msg)
		}
		if strings.Contains(msg, key) {
			t.Errorf("mode %o: the refusal prints the key: %q", mode, msg)
		}
	}
}

// TestAFileOwnerOnlyIsUsed checks the other side of the mode rule: 0600 is read.
func TestAFileOwnerOnlyIsUsed(t *testing.T) {
	key := newKey(t)
	home := keyEnv(t, "")
	writeKeyFile(t, home, 0o600, "CAIRN_API_KEY="+key+"\n")
	k, err := client.LoadKey()
	if err != nil || k.Value != key || k.Source != client.SourceFile {
		t.Errorf("LoadKey = %v from %s, %v; want the file's key", k, k.Source, err)
	}
}

// TestNoKeyInEitherSourceIsMissing checks that a key found nowhere is not an error. The
// returned key has no source, and the message the commands print names both places.
func TestNoKeyInEitherSourceIsMissing(t *testing.T) {
	keyEnv(t, "")
	k, err := client.LoadKey()
	if err != nil || k.Source != "" || k.Value != "" {
		t.Fatalf("LoadKey with no key = %v, %v; want nothing and no error", k, err)
	}
	msg := client.NoKeyMessage()
	if !strings.Contains(msg, client.EnvKey) || !strings.Contains(msg, "client.env") {
		t.Errorf("NoKeyMessage %q does not name both sources", msg)
	}
}

// TestClientEnvFileIsParsed checks the file format: the last CAIRN_API_KEY line wins, other names
// and comments are ignored, and one pair of quotes is removed. A line with no '=' is refused by
// its number, and the refusal does not print its text, since that text may be a key.
func TestClientEnvFileIsParsed(t *testing.T) {
	want := newKey(t)
	home := keyEnv(t, "")
	writeKeyFile(t, home, 0o600, "# comment\n\nOTHER=1\nCAIRN_API_KEY=old\nCAIRN_API_KEY='"+want+"'\n")
	k, err := client.LoadKey()
	if err != nil || k.Value != want {
		t.Errorf("last line: LoadKey = %v, %v; want the last key", k, err)
	}

	pasted := newKey(t)
	writeKeyFile(t, home, 0o600, pasted+"\n")
	_, err = client.LoadKey()
	if err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Errorf("a bare key line: err = %v, want a refusal naming line 1", err)
	}
	if err != nil && strings.Contains(err.Error(), pasted) {
		t.Errorf("the refusal prints the pasted key: %v", err)
	}
}

// TestTheHeaderIsSentOnEveryCall checks that a keyed client sends Authorization: Bearer <key>
// on each call, and that a client made with New sends none. The server here is the handler
// with the header-trusting caller, which ignores the key, so the calls succeed and the test
// reads only what was sent.
func TestTheHeaderIsSentOnEveryCall(t *testing.T) {
	key := newKey(t)
	var mu sync.Mutex
	var seen []string
	h := api.Handler(newLocal(t), api.UnauthenticatedHeaderCaller, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	keyed, err := client.NewWithKey(srv.URL, key, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := keyed.TaskCurrent(agentCtx()); err != nil {
			t.Fatalf("keyed call: %v", err)
		}
	}
	bare, err := client.New(srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bare.TaskCurrent(agentCtx()); err != nil {
		t.Fatalf("bare call: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Fatalf("the server saw %d calls, want 3", len(seen))
	}
	for i, got := range seen[:2] {
		if got != "Bearer "+key {
			t.Errorf("keyed call %d sent Authorization %q, want the bearer key", i+1, got)
		}
	}
	if seen[2] != "" {
		t.Errorf("a client made with New sent Authorization %q, want none", seen[2])
	}
}

// TestA401SaysTheServerRefusedTheKey checks the message for a refused key: it says the server
// refused the API key, names where the key comes from, and does not print the key.
func TestA401SaysTheServerRefusedTheKey(t *testing.T) {
	key := newKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"kind":"unauthenticated","message":"unauthenticated"}}`))
	}))
	t.Cleanup(srv.Close)
	c, err := client.NewWithKey(srv.URL, key, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.TaskCurrent(agentCtx())
	if err == nil {
		t.Fatal("a 401 gave no error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "the server refused the API key") || !strings.Contains(msg, "CAIRN_API_KEY") {
		t.Errorf("401 message %q does not say the server refused the key and where it comes from", msg)
	}
	if strings.Contains(msg, key) {
		t.Errorf("401 message prints the key: %q", msg)
	}
}

// TestNewWithKeyRefusesAKeyAHeaderCannotCarry checks that a key with a space or a line break is
// refused before anything is sent, and that the refusal does not print the key.
func TestNewWithKeyRefusesAKeyAHeaderCannotCarry(t *testing.T) {
	key := newKey(t)
	for _, bad := range []string{key + " ", key + "\n", key[:10] + " " + key[10:]} {
		_, err := client.NewWithKey("http://127.0.0.1:1", bad, nil)
		if err == nil {
			t.Errorf("NewWithKey accepted a key with a space or a line break")
			continue
		}
		if strings.Contains(err.Error(), key) {
			t.Errorf("the refusal prints the key: %v", err)
		}
	}
}

// TestKeyTextFormsHideTheValue checks that a Key printed with any verb shows its source and not
// its value.
func TestKeyTextFormsHideTheValue(t *testing.T) {
	key := newKey(t)
	k := client.Key{Value: key, Source: client.SourceEnv}
	for _, s := range []string{k.String(), k.GoString()} {
		if strings.Contains(s, key) {
			t.Errorf("a printed Key shows the value: %q", s)
		}
	}
}
