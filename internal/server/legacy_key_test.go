package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/store"
)

// addLegacyKey stores an active key named name with scope, in the form a key had before the rename to
// Cairnbook: the legacy prefix, then the body of a key issued now. The store keeps only its hash, as
// for any key, and the plaintext is returned to the test alone.
func addLegacyKey(t *testing.T, st *store.Store, name string, scope auth.Scope) string {
	t.Helper()
	fresh, err := auth.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	key := auth.LegacyKeyPrefix + strings.TrimPrefix(fresh, auth.KeyPrefix)
	if err := st.WithTx(context.Background(), func(q store.Querier) error {
		_, err := store.AddAPIKey(context.Background(), q, store.APIKeyNew{
			Name: name, Hash: auth.HashKey(key), Scope: string(scope), CreatedAt: time.Now(),
		})
		return err
	}); err != nil {
		t.Fatalf("add key %s: %v", name, err)
	}
	return key
}

// TestLegacyKeyAuthenticatesAnAPICall checks that a key stored under the old prefix opens a call to the
// HTTP API, through the server's handler on a real listener: a task started with it is read back.
func TestLegacyKeyAuthenticatesAnAPICall(t *testing.T) {
	st := openStore(t)
	key := addLegacyKey(t, st, "ci", auth.ScopeAgent)
	base := startServer(t, Handler(st, testLogger(io.Discard)), nil)
	c := client(nil)

	resp, body := send(t, c, http.MethodPost, base+"/v1/agent/task_start",
		`{"title":"legacy key","goal":"a key issued before the rename"}`, callHeaders(key))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("task_start with a legacy key = %d: %s", resp.StatusCode, body)
	}
	resp, body = send(t, c, http.MethodPost, base+"/v1/agent/task_current", "", callHeaders(key))
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "legacy key") {
		t.Errorf("task_current with a legacy key = %d: %s; want the task just started", resp.StatusCode, body)
	}
}

// TestLegacyKeySignsInToTheWebUI checks that a legacy operator key signs in to the web UI over the same
// listener: the sign-in answers 303 with the session cookie, and the dashboard the cookie opens names the
// key and its role.
func TestLegacyKeySignsInToTheWebUI(t *testing.T) {
	st := openStore(t)
	key := addLegacyKey(t, st, "ops", auth.ScopeOperator)
	base := startServer(t, Handler(st, testLogger(io.Discard)), nil)
	// The sign-in answer is read as it is, not followed to the dashboard.
	c := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp, body := send(t, c, http.MethodPost, base+"/ui/signin", "key="+key, map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
		"Origin":       base,
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign-in with a legacy key = %d: %s; want 303", resp.StatusCode, body)
	}
	var session string
	for _, ck := range resp.Cookies() {
		if ck.Name == "cairnbook_ui" {
			session = ck.Value
		}
	}
	if session == "" {
		t.Fatalf("sign-in set no cairnbook_ui cookie: %v", resp.Cookies())
	}

	resp, body = send(t, c, http.MethodGet, base+"/ui/", "", map[string]string{
		"Cookie": "cairnbook_ui=" + session,
	})
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "ops (operator)") {
		t.Errorf("dashboard with the session = %d; want 200 naming ops (operator): %s", resp.StatusCode, body)
	}
}
