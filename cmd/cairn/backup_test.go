package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/store"
)

// makeServerDB makes a database at path with one key, as cairn key add would, and closes it.
func makeServerDB(t *testing.T, path string) {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer st.Close()
	err = st.WithTx(context.Background(), func(q store.Querier) error {
		_, err := store.AddAPIKey(context.Background(), q, store.APIKeyNew{
			Name: "ops", Hash: "hash-ops", Scope: "operator", CreatedAt: time.Now().UTC(),
		})
		return err
	})
	if err != nil {
		t.Fatalf("add a key: %v", err)
	}
}

// TestBackupCommandWritesACopyAndReportsIt checks the success line: the database, the copy and its
// size, on stdout, with the copy at the mode it was made with.
func TestBackupCommandWritesACopyAndReportsIt(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cairn.db")
	makeServerDB(t, db)
	out := filepath.Join(dir, "backup.db")
	code, stdout, stderr := runCmd(t, "backup", "--db", db, out)
	if code != 0 {
		t.Fatalf("backup exit %d, stderr %q", code, stderr)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	want := "backed up " + db + " to " + out + " ("
	if !strings.HasPrefix(stdout, want) || !strings.Contains(stdout, " bytes)") {
		t.Errorf("stdout = %q, want the database, the copy and the size", stdout)
	}
	if info.Size() == 0 {
		t.Errorf("the copy is empty")
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("the copy's mode = %04o, want 0600", info.Mode().Perm())
	}
}

// TestBackupCommandUsageAndRefusals checks the usage errors (exit 2) and the refusals of the
// server package (exit 1), with the message on stderr.
func TestBackupCommandUsageAndRefusals(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cairn.db")
	makeServerDB(t, db)
	existing := filepath.Join(dir, "exists.db")
	if err := os.WriteFile(existing, []byte("taken"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{"no --db", []string{"backup", filepath.Join(dir, "a.db")}, 2, "needs --db"},
		{"no output file", []string{"backup", "--db", db}, 2, "needs one output file"},
		{"two output files", []string{"backup", "--db", db, "a.db", "b.db"}, 2, "needs one output file"},
		{"an existing output", []string{"backup", "--db", db, existing}, 1, "already exists"},
		{"a memory output", []string{"backup", "--db", db, ":memory:"}, 1, "memory database"},
		{"a missing database", []string{"backup", "--db", filepath.Join(dir, "none.db"), filepath.Join(dir, "x.db")}, 1, "no database at"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, stderr := runCmd(t, c.args...)
			if code != c.wantCode || !strings.Contains(stderr, c.wantErr) {
				t.Errorf("exit %d, stderr %q; want exit %d and %q", code, stderr, c.wantCode, c.wantErr)
			}
		})
	}
	if b, _ := os.ReadFile(existing); string(b) != "taken" {
		t.Errorf("a refused backup changed the existing file")
	}
}

// TestBackupCommandHelpGoesToStdout checks that --help is a success on stdout, as the other
// commands are.
func TestBackupCommandHelpGoesToStdout(t *testing.T) {
	code, stdout, stderr := runCmd(t, "backup", "--help")
	if code != 0 || !strings.Contains(stdout, "Usage: cairn backup") || stderr != "" {
		t.Errorf("backup --help: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}
