package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// restoreSeams sets the confirmation's reader and the terminal check for one test, and puts the
// real ones back when it ends.
func restoreSeams(t *testing.T, input io.Reader, terminal bool) {
	t.Helper()
	prevInput, prevTerminal := restoreInput, stdinIsTerminal
	restoreInput = input
	stdinIsTerminal = func() bool { return terminal }
	t.Cleanup(func() {
		restoreInput = prevInput
		stdinIsTerminal = prevTerminal
	})
}

// TestRestoreCommandRefusesANonTerminal checks that with stdin not a terminal, a correctly typed
// path is still refused and the database is untouched (M2 #74, step c).
func TestRestoreCommandRefusesANonTerminal(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cairn.db")
	makeServerDB(t, db)
	backup := filepath.Join(dir, "backup.db")
	if code, _, stderr := runCmd(t, "backup", "--db", db, backup); code != 0 {
		t.Fatalf("make the backup: exit %d, %q", code, stderr)
	}
	before, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}

	restoreSeams(t, strings.NewReader(db+"\n"), false)
	code, _, stderr := runCmd(t, "restore", "--db", db, backup)
	if code != 1 || !strings.Contains(stderr, "not a terminal") {
		t.Errorf("restore with stdin not a terminal: exit %d, stderr %q; want exit 1 and the not-a-terminal refusal", code, stderr)
	}
	if after, _ := os.ReadFile(db); string(after) != string(before) {
		t.Errorf("a refused restore changed the database")
	}
	if matches, _ := filepath.Glob(db + ".pre-restore-*"); len(matches) != 0 {
		t.Errorf("a refused restore made %v", matches)
	}
}

// TestRestoreCommandRestoresAtATerminal checks the whole command with a terminal and the exact path
// typed: the restore exits 0, names what moved, and the old database is kept.
func TestRestoreCommandRestoresAtATerminal(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cairn.db")
	makeServerDB(t, db)
	backup := filepath.Join(dir, "backup.db")
	if code, _, stderr := runCmd(t, "backup", "--db", db, backup); code != 0 {
		t.Fatalf("make the backup: exit %d, %q", code, stderr)
	}

	restoreSeams(t, strings.NewReader(db+"\n"), true)
	code, stdout, stderr := runCmd(t, "restore", "--db", db, backup)
	if code != 0 {
		t.Fatalf("restore: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "moved "+db+" to "+db+".pre-restore-") || !strings.Contains(stdout, "restored "+db) {
		t.Errorf("the report %q does not say what moved where", stdout)
	}
	// The -wal is folded into the database before the move, so no pre-restore -wal is kept. A -shm
	// may be moved beside the database, and the report must name it. The count is of the database
	// files, and each one must be named as a move.
	matches, err := filepath.Glob(db + ".pre-restore-*")
	var main []string
	for _, m := range matches {
		if strings.HasSuffix(m, "-wal") {
			t.Errorf("a pre-restore -wal was kept: %s", m)
		}
		if !strings.HasSuffix(m, "-shm") {
			main = append(main, m)
		}
		if !strings.Contains(stdout, " to "+m+"\n") {
			t.Errorf("the report %q does not name the move to %s", stdout, m)
		}
	}
	if err != nil || len(main) != 1 {
		t.Errorf("pre-restore database files = %v (%v), want exactly one", main, err)
	}
}

// TestRestoreCommandNamesTheNoDatabasePrompt checks 14-S3: restoring into a path with no database
// says so in the prompt, and does not say a database is moved.
func TestRestoreCommandNamesTheNoDatabasePrompt(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "fresh.db")
	makeServerDB(t, filepath.Join(dir, "src.db"))
	backup := filepath.Join(dir, "backup.db")
	if code, _, stderr := runCmd(t, "backup", "--db", filepath.Join(dir, "src.db"), backup); code != 0 {
		t.Fatalf("make the backup: exit %d, %q", code, stderr)
	}

	restoreSeams(t, strings.NewReader(db+"\n"), true)
	code, stdout, stderr := runCmd(t, "restore", "--db", db, backup)
	if code != 0 {
		t.Fatalf("restore into an absent path: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "No database is at "+db) || strings.Contains(stdout, "The database there now is moved") {
		t.Errorf("the prompt for an absent database = %q, want it to say no database is there", stdout)
	}
}

// TestRestoreCommandUsageRefusals checks the usage errors, which exit 2 before anything is read.
func TestRestoreCommandUsageRefusals(t *testing.T) {
	dir := t.TempDir()
	restoreSeams(t, strings.NewReader(""), true)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no --db", []string{"restore", filepath.Join(dir, "b.db")}, "needs --db"},
		{"no backup", []string{"restore", "--db", filepath.Join(dir, "c.db")}, "needs one backup file"},
		{"two backups", []string{"restore", "--db", filepath.Join(dir, "c.db"), "a", "b"}, "needs one backup file"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, stderr := runCmd(t, c.args...)
			if code != 2 || !strings.Contains(stderr, c.want) {
				t.Errorf("exit %d, stderr %q; want exit 2 and %q", code, stderr, c.want)
			}
		})
	}
}

// TestRestoreCommandHelpGoesToStdout checks that --help is a success on stdout.
func TestRestoreCommandHelpGoesToStdout(t *testing.T) {
	code, stdout, stderr := runCmd(t, "restore", "--help")
	if code != 0 || !strings.Contains(stdout, "Usage: cairn restore") || stderr != "" {
		t.Errorf("restore --help: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}
