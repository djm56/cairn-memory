package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/auth"
)

// dbFiles returns the database and the -wal and -shm files beside it that exist now.
func dbFiles(t *testing.T, path string) []string {
	t.Helper()
	var out []string
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(f); err == nil {
			out = append(out, f)
		}
	}
	return out
}

// TestAddKeyStoresOnlyTheHash checks that AddKey returns the plaintext once, in the key
// format, and that the plaintext appears in none of the database's files, while its hash
// does. A key a database could show again would be a key an operator could lose to a copy.
func TestAddKeyStoresOnlyTheHash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cairn.db")
	key, err := AddKey(context.Background(), path, "ci", auth.ScopeAgent)
	if err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	if !strings.HasPrefix(key, auth.KeyPrefix) || len(key) != len(auth.KeyPrefix)+43 {
		t.Fatalf("AddKey returned %q, which is not a key in the NewKey format", key)
	}
	// The hash may sit in the main file or in the -wal file, depending on checkpointing, so
	// the check reads every file that exists together.
	all := ""
	for _, f := range dbFiles(t, path) {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), key) {
			t.Errorf("%s holds the plaintext key", filepath.Base(f))
		}
		all += string(b)
	}
	if !strings.Contains(all, auth.HashKey(key)) {
		t.Error("no database file holds the hash of the key")
	}
}

// TestAddKeyRefusesABadNameAndScope checks the refusals of AddKey. A name that begins with the
// key prefix is refused without being echoed, so a key pasted into the name slot is not
// printed back and is not stored.
func TestAddKeyRefusesABadNameAndScope(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cairn.db")
	pasted, err := auth.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		what  string
		name  string
		scope auth.Scope
	}{
		{"an empty name", "", auth.ScopeAgent},
		{"a name with a space", "a b", auth.ScopeAgent},
		{"a name that starts with a hyphen", "-ci", auth.ScopeAgent},
		{"a name of 65 characters", strings.Repeat("a", 65), auth.ScopeAgent},
		{"a key pasted as the name", pasted, auth.ScopeAgent},
		{"a scope that is not one of three", "ci", auth.Scope("admin")},
	}
	for _, c := range cases {
		_, err := AddKey(ctx, path, c.name, c.scope)
		if err == nil {
			t.Errorf("%s: AddKey accepted it", c.what)
			continue
		}
		if strings.Contains(err.Error(), pasted) {
			t.Errorf("%s: the error echoes the key: %v", c.what, err)
		}
	}
	if _, err := AddKey(ctx, path, strings.Repeat("a", 64), auth.ScopeViewer); err != nil {
		t.Errorf("a 64-character name was refused: %v", err)
	}
}

// TestAddKeyRefusesANameThatIsTaken checks that a name stays taken after its key is revoked,
// since the store keeps the row. A new key cannot take an old name.
func TestAddKeyRefusesANameThatIsTaken(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cairn.db")
	if _, err := AddKey(ctx, path, "ci", auth.ScopeAgent); err != nil {
		t.Fatalf("first AddKey: %v", err)
	}
	if _, err := AddKey(ctx, path, "ci", auth.ScopeOperator); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a second key named ci: err = %v, want a name already taken", err)
	}
	if err := RevokeKey(ctx, path, "ci"); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}
	if _, err := AddKey(ctx, path, "ci", auth.ScopeOperator); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a key named after a revoked one: err = %v, want the name still taken", err)
	}
}

// TestListKeysShowsEveryKeyInOrder checks the rows cairn key list prints: every key, the
// revoked one included, in the order they were made, with the revocation time on the one
// revoked. KeyInfo has no hash or plaintext field, so none can be printed.
func TestListKeysShowsEveryKeyInOrder(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cairn.db")
	if _, err := AddKey(ctx, path, "ci", auth.ScopeAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := AddKey(ctx, path, "ops", auth.ScopeOperator); err != nil {
		t.Fatal(err)
	}
	if err := RevokeKey(ctx, path, "ci"); err != nil {
		t.Fatal(err)
	}
	keys, err := ListKeys(ctx, path)
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 2 || keys[0].Name != "ci" || keys[1].Name != "ops" {
		t.Fatalf("ListKeys = %+v, want ci then ops", keys)
	}
	if keys[0].Scope != auth.ScopeAgent || keys[1].Scope != auth.ScopeOperator {
		t.Errorf("scopes = %s, %s; want agent, operator", keys[0].Scope, keys[1].Scope)
	}
	if keys[0].RevokedAt == nil || keys[1].RevokedAt != nil {
		t.Errorf("revoked_at: ci %v, ops %v; want set for ci only", keys[0].RevokedAt, keys[1].RevokedAt)
	}
	if keys[0].CreatedAt.IsZero() || keys[1].LastUsedAt != nil {
		t.Errorf("created_at is zero or last_used_at set on an unused key: %+v", keys[1])
	}
}

// TestRevokeKeyTwiceIsNotFound checks that a second revocation, and a name with no key, are
// errors that say so, and that the first revocation stands.
func TestRevokeKeyTwiceIsNotFound(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cairn.db")
	if _, err := AddKey(ctx, path, "ci", auth.ScopeAgent); err != nil {
		t.Fatal(err)
	}
	if err := RevokeKey(ctx, path, "ci"); err != nil {
		t.Fatalf("first revoke: %v", err)
	}
	for _, name := range []string{"ci", "nobody"} {
		err := RevokeKey(ctx, path, name)
		if err == nil || !strings.Contains(err.Error(), "no active API key named "+name) {
			t.Errorf("revoke %s again: err = %v, want no active key named %s", name, err, name)
		}
	}
}

// TestKeyCommandsNeedADatabaseThatExists checks that list and revoke name a missing database
// and do not create one, so a mistyped path is an error and not a new empty database. A
// memory database is refused for the commands too.
func TestKeyCommandsNeedADatabaseThatExists(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "absent.db")
	if _, err := ListKeys(ctx, path); err == nil || !strings.Contains(err.Error(), "no database at") {
		t.Errorf("ListKeys on a missing file: err = %v, want no database at", err)
	}
	if err := RevokeKey(ctx, path, "ci"); err == nil || !strings.Contains(err.Error(), "no database at") {
		t.Errorf("RevokeKey on a missing file: err = %v, want no database at", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a missing file was created by a list or revoke: stat err = %v", err)
	}
	if _, err := AddKey(ctx, ":memory:", "ci", auth.ScopeAgent); err == nil || !strings.Contains(err.Error(), "must be a file path") {
		t.Errorf("AddKey on :memory: err = %v, want the file-path refusal", err)
	}
}

// TestRevokedKeyStopsWorkingOnARunningServer serves a database over a real listener, makes
// a key, and checks that the key is accepted, then revoked from a second handle on the same
// file, and refused at once by the server that is still running. The server reads the key
// table on every call, so no restart is needed.
func TestRevokedKeyStopsWorkingOnARunningServer(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cairn.db")
	key, err := AddKey(ctx, path, "ci", auth.ScopeAgent)
	if err != nil {
		t.Fatal(err)
	}
	st, err := openDatabase(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	base := startServer(t, Handler(st, testLogger(io.Discard)), nil)
	c := client(nil)

	resp, body := send(t, c, http.MethodPost, base+"/v1/agent/task_current", "", callHeaders(key))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("before revoke: status %d: %s", resp.StatusCode, body)
	}
	if err := RevokeKey(ctx, path, "ci"); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}
	resp, body = send(t, c, http.MethodPost, base+"/v1/agent/task_current", "", callHeaders(key))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("after revoke: status %d: %s; want 401", resp.StatusCode, body)
	}
}
