package server

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"time"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/store"
)

// TestRevokeKeyRefusesAKeyPastedAsAName checks that a key given where a name belongs is refused
// by the name rule, the refusal does not print the key, and no key is revoked by the call. The
// check runs before the database is opened, as add's does.
func TestRevokeKeyRefusesAKeyPastedAsAName(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "cairnbook.db")
	if _, err := AddKey(ctx, db, "ci", auth.ScopeAgent); err != nil {
		t.Fatalf("AddKey ci: %v", err)
	}
	pasted, err := AddKey(ctx, db, "other", auth.ScopeAgent)
	if err != nil {
		t.Fatalf("AddKey other: %v", err)
	}

	err = RevokeKey(ctx, db, pasted)
	if err == nil || !strings.Contains(err.Error(), "looks like an API key") {
		t.Fatalf("RevokeKey with a key as the name = %v, want the name rule", err)
	}
	if strings.Contains(err.Error(), pasted) {
		t.Errorf("the refusal prints the key: %v", err)
	}
	keys, err := ListKeys(ctx, db)
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	for _, k := range keys {
		if k.RevokedAt != nil {
			t.Errorf("key %s was revoked by a call that named a key", k.Name)
		}
	}
}

// TestRevokeKeyRefusesABadNameBeforeTheLookup checks that a name the rule refuses is refused as a
// name, not as a missing key, whatever the database holds: the lookup never runs.
func TestRevokeKeyRefusesABadNameBeforeTheLookup(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cairnbook.db")
	if _, err := AddKey(context.Background(), db, "ci", auth.ScopeAgent); err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	err := RevokeKey(context.Background(), db, "has a space")
	if err == nil || strings.Contains(err.Error(), "no active API key") {
		t.Errorf("RevokeKey with a bad name = %v, want the name rule and not a lookup failure", err)
	}
}

// TestDanglingLinkAtTheDatabaseIsRefused checks that a symbolic link at the database path to a
// file that does not exist is refused by name, and nothing is created at its target. A link to a
// file that exists is opened, as it was before.
func TestDanglingLinkAtTheDatabaseIsRefused(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere.db")
	link := filepath.Join(dir, "cairnbook.db")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := AddKey(context.Background(), link, "ci", auth.ScopeAgent)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("AddKey through a dangling link = %v, want the dangling-link refusal", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the link's target was created: stat err %v", err)
	}

	// A link to a file that exists is opened: the mode is the target's, which is 0600 here.
	real := filepath.Join(dir, "real.db")
	if err := createDatabaseFile(real); err != nil {
		t.Fatalf("createDatabaseFile: %v", err)
	}
	live := filepath.Join(dir, "live.db")
	if err := os.Symlink(real, live); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := AddKey(context.Background(), live, "ci", auth.ScopeAgent); err != nil {
		t.Errorf("AddKey through a link to an existing file: %v", err)
	}
}

// TestCheckKeyNameRefusesAKeyOfEitherPrefix checks that a key pasted into the name position is
// refused whether it was issued before the rename or after, and that the refusal does not print
// the name.
func TestCheckKeyNameRefusesAKeyOfEitherPrefix(t *testing.T) {
	for _, name := range []string{auth.KeyPrefix + "abc", auth.LegacyKeyPrefix + "abc"} {
		err := checkKeyName(name)
		if err == nil || !strings.Contains(err.Error(), "must not begin with cairnbook_") {
			t.Errorf("checkKeyName(%q) = %v, want the name rule", name, err)
		}
		if err != nil && strings.Contains(err.Error(), name) {
			t.Errorf("the refusal prints the name: %v", err)
		}
	}
	if err := checkKeyName("ci-cairn"); err != nil {
		t.Errorf("checkKeyName(ci-cairn) = %v, want no error", err)
	}
}

// insertKeyNamed writes an active key named name straight through the store, for the names
// AddKey refuses. The database must be a path; it is created when absent.
func insertKeyNamed(t *testing.T, db, name string) {
	t.Helper()
	ctx := context.Background()
	key, err := auth.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	st, err := openDatabase(db, true)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	err = st.WithTx(ctx, func(q store.Querier) error {
		_, err := store.AddAPIKey(ctx, q, store.APIKeyNew{
			Name: name, Hash: auth.HashKey(key), Scope: string(auth.ScopeAgent), CreatedAt: time.Now().UTC(),
		})
		return err
	})
	if err != nil {
		t.Fatalf("insert key %s: %v", name, err)
	}
}

// TestRevokeKeyAcceptsANameThatOnlyBeginsWithAPrefix checks that a key whose name begins with
// either prefix, which AddKey refuses but an unreleased renamed build may have stored, can be
// revoked by that name. Only a name shaped like a whole key is refused on revoke.
func TestRevokeKeyAcceptsANameThatOnlyBeginsWithAPrefix(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "cairnbook.db")
	names := []string{"cairn_x", "cairnbook_x"}
	for _, name := range names {
		insertKeyNamed(t, db, name)
	}
	for _, name := range names {
		if err := RevokeKey(ctx, db, name); err != nil {
			t.Errorf("RevokeKey %s: %v; want it revoked", name, err)
		}
	}
	keys, err := ListKeys(ctx, db)
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	for _, k := range keys {
		if k.RevokedAt == nil {
			t.Errorf("key %s is not revoked after RevokeKey", k.Name)
		}
	}
}

// TestRevokeKeyRefusesAFullKeyOfEitherPrefix checks that a name shaped like a whole key, under
// either prefix, is refused on revoke with the shape message, and that the refusal does not print
// the name. The names are built here, not issued, so the test holds whatever the store contains.
func TestRevokeKeyRefusesAFullKeyOfEitherPrefix(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "cairnbook.db")
	if _, err := AddKey(ctx, db, "ci", auth.ScopeAgent); err != nil {
		t.Fatalf("AddKey ci: %v", err)
	}
	for _, prefix := range []string{auth.KeyPrefix, auth.LegacyKeyPrefix} {
		name := prefix + strings.Repeat("B", 43)
		err := RevokeKey(ctx, db, name)
		if err == nil || !strings.Contains(err.Error(), "looks like an API key") {
			t.Errorf("RevokeKey with a %s-shaped name = %v, want the shape refusal", prefix, err)
		}
		if err != nil && strings.Contains(err.Error(), name) {
			t.Errorf("the refusal prints the name: %v", err)
		}
	}
}

// TestRevokeKeyStillRefusesAMalformedName checks that a name failing keyNamePattern is refused by
// the name rule on revoke, before the lookup, and that the rule given is the revoke rule, which
// names no prefix.
func TestRevokeKeyStillRefusesAMalformedName(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "cairnbook.db")
	if _, err := AddKey(ctx, db, "ci", auth.ScopeAgent); err != nil {
		t.Fatalf("AddKey ci: %v", err)
	}
	for _, name := range []string{"has a space", "-cairn", strings.Repeat("a", 65)} {
		err := RevokeKey(ctx, db, name)
		if err == nil {
			t.Errorf("RevokeKey with malformed name %q = nil, want the name rule", name)
			continue
		}
		if !strings.Contains(err.Error(), "must be 1 to 64 letters") {
			t.Errorf("RevokeKey with a malformed name = %v, want the name rule", err)
		}
		if strings.Contains(err.Error(), "cairnbook_") || strings.Contains(err.Error(), "cairn_") {
			t.Errorf("the revoke refusal names a prefix: %v", err)
		}
	}
}

// echoesRun reports whether msg holds a run of minRun consecutive characters of input. A longer
// run in msg implies a minRun-long run from its start in input, so checking the windows of
// length minRun is enough.
func echoesRun(msg, input string, minRun int) bool {
	for i := 0; i+minRun <= len(input); i++ {
		if strings.Contains(msg, input[i:i+minRun]) {
			return true
		}
	}
	return false
}

// TestRevokeKeyNotFoundNeverEchoesTheName checks that a key-shaped name which the shape test does
// not refuse, one character too long, one too short, or with no prefix, reaches the lookup, and
// that the not-found error holds no run of it longer than the prefix. The prefix alone is allowed
// to appear, since the refusal text names the command, not the prefix; the body and its first ten
// characters must not.
func TestRevokeKeyNotFoundNeverEchoesTheName(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "cairnbook.db")
	if _, err := AddKey(ctx, db, "ci", auth.ScopeAgent); err != nil {
		t.Fatalf("AddKey ci: %v", err)
	}
	key, err := auth.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	body := strings.TrimPrefix(key, auth.KeyPrefix)
	type echoCase struct{ what, name string }
	var cases []echoCase
	for _, prefix := range []string{auth.KeyPrefix, auth.LegacyKeyPrefix} {
		for _, extra := range []string{".", "-", "_", "x"} {
			cases = append(cases, echoCase{"a " + prefix + " key with a trailing " + extra, prefix + body + extra})
		}
		cases = append(cases, echoCase{"a " + prefix + " key one character short", prefix + body[:len(body)-1]})
	}
	cases = append(cases, echoCase{"a 43-character body with no prefix", body})

	minRun := len(auth.KeyPrefix) + 1
	for _, c := range cases {
		err := RevokeKey(ctx, db, c.name)
		if err == nil || !strings.Contains(err.Error(), "no active API key has that name") {
			t.Errorf("RevokeKey with %s = %v, want the not-found error", c.what, err)
			continue
		}
		msg := err.Error()
		if strings.Contains(msg, c.name) || echoesRun(msg, c.name, minRun) {
			t.Errorf("the not-found error for %s echoes the name: %v", c.what, err)
		}
		if strings.Contains(msg, body) || strings.Contains(msg, body[:10]) {
			t.Errorf("the not-found error for %s echoes the key body: %v", c.what, err)
		}
	}
}

// TestRevokeKeyNotFoundDoesNotEchoAnOrdinaryName checks that the not-found error for a name with
// no key does not repeat that name, and still says that no key has it.
func TestRevokeKeyNotFoundDoesNotEchoAnOrdinaryName(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "cairnbook.db")
	if _, err := AddKey(ctx, db, "ci", auth.ScopeAgent); err != nil {
		t.Fatalf("AddKey ci: %v", err)
	}
	err := RevokeKey(ctx, db, "nobody")
	if err == nil || !strings.Contains(err.Error(), "no active API key has that name") {
		t.Fatalf("RevokeKey nobody = %v, want the not-found error", err)
	}
	if strings.Contains(err.Error(), "nobody") {
		t.Errorf("the not-found error repeats the name: %v", err)
	}
}
