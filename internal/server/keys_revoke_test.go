package server

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/auth"
)

// TestRevokeKeyRefusesAKeyPastedAsAName checks 8-W6: a key given where a name belongs is refused
// by the name rule, the refusal does not print the key, and no key is revoked by the call. The
// check runs before the database is opened, as add's does.
func TestRevokeKeyRefusesAKeyPastedAsAName(t *testing.T) {
	ctx := context.Background()
	db := filepath.Join(t.TempDir(), "cairn.db")
	if _, err := AddKey(ctx, db, "ci", auth.ScopeAgent); err != nil {
		t.Fatalf("AddKey ci: %v", err)
	}
	pasted, err := AddKey(ctx, db, "other", auth.ScopeAgent)
	if err != nil {
		t.Fatalf("AddKey other: %v", err)
	}

	err = RevokeKey(ctx, db, pasted)
	if err == nil || !strings.Contains(err.Error(), "must not begin with cairn_") {
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
	db := filepath.Join(t.TempDir(), "cairn.db")
	if _, err := AddKey(context.Background(), db, "ci", auth.ScopeAgent); err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	err := RevokeKey(context.Background(), db, "has a space")
	if err == nil || strings.Contains(err.Error(), "no active API key") {
		t.Errorf("RevokeKey with a bad name = %v, want the name rule and not a lookup failure", err)
	}
}

// TestDanglingLinkAtTheDatabaseIsRefused checks 8-S3: a symbolic link at the database path to a
// file that does not exist is refused by name, and nothing is created at its target. A link to a
// file that exists is opened, as it was before.
func TestDanglingLinkAtTheDatabaseIsRefused(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere.db")
	link := filepath.Join(dir, "cairn.db")
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
