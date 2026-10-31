package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/djm56/cairn-memory/internal/auth"
)

// TestNewDatabaseAndItsFilesAreOwnerOnly checks 6-S3: a database the server makes, and the
// -wal and -shm files SQLite makes beside it, are 0600. The files exist only while the store
// is open, so the check runs before Close. A row is written first, so the -wal file exists.
//
// SQLite gives -wal and -shm the mode of the database file it is opened beside, and the
// umask can only remove bits from that, so the mode set at create carries to them.
func TestNewDatabaseAndItsFilesAreOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cairn.db")
	st, err := openDatabase(path, true)
	if err != nil {
		t.Fatalf("openDatabase: %v", err)
	}
	defer st.Close()
	addKey(t, st, "ci", auth.ScopeAgent)
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(f)
		if err != nil {
			t.Fatalf("stat %s: %v", f, err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s has mode %o, want 600", filepath.Base(f), got)
		}
	}
}

// TestOpenDatabaseLeavesAnExistingFileAlone checks that opening a database that is already
// there does not change its mode. Only a file the server creates is made 0600.
func TestOpenDatabaseLeavesAnExistingFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cairn.db")
	if _, err := AddKey(context.Background(), path, "ci", auth.ScopeAgent); err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := openDatabase(path, true)
	if err != nil {
		t.Fatalf("openDatabase: %v", err)
	}
	st.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("an existing database changed to mode %o, want 644 left as it was", got)
	}
}

// TestCheckDBPath checks the names a database may have. The store appends its options after
// a '?', and a memory database or a file URI loses its data at exit (6-S10).
func TestCheckDBPath(t *testing.T) {
	cases := []struct {
		path string
		ok   bool
	}{
		{"cairn.db", true},
		{"/var/lib/cairn/cairn.db", true},
		{"", false},
		{"cairn.db?mode=ro", false},
		{":memory:", false},
		{"file::memory:", false},
		{"file:cairn.db", false},
	}
	for _, c := range cases {
		if err := checkDBPath(c.path); (err == nil) != c.ok {
			t.Errorf("checkDBPath(%q) = %v, want ok=%v", c.path, err, c.ok)
		}
	}
}
