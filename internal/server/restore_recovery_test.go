package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/store"
)

// printedCommand returns the shell command a restore error tells the operator to run. The text
// after "then run: " is the command, and it ends before the README reference.
func printedCommand(t *testing.T, msg string) string {
	t.Helper()
	const lead, tail = "then run: ", " (see README"
	i := strings.Index(msg, lead)
	j := strings.LastIndex(msg, tail)
	if i < 0 || j < i {
		t.Fatalf("the error gives no recovery command: %q", msg)
	}
	return msg[i+len(lead) : j]
}

// TestRecoveryStepPrintsACommandThatMovesTheFilesBack is the proof for 14b-W2. The database path has
// a space and a single quote in it. A restore fails after its moves, and the command the error
// prints, run as printed under sh, puts every moved file back under its name.
func TestRecoveryStepPrintsACommandThatMovesTheFilesBack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my data", "it's here")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn 'one'.db")
	seedDB(t, target, "beta")
	leaveWAL(t, target, "gamma")

	prev := placeCopy
	placeCopy = func(string, string) error { return errors.New("injected place failure") }
	t.Cleanup(func() { placeCopy = prev })

	opts, _ := restoreRun(target, backup, target+"\n", true)
	err := Restore(context.Background(), opts)
	if err == nil {
		t.Fatal("Restore with the place step failing returned nil")
	}
	cmd := printedCommand(t, err.Error())
	t.Logf("printed command: %s", cmd)

	if out, rerr := exec.Command("sh", "-c", cmd).CombinedOutput(); rerr != nil {
		t.Fatalf("the printed command failed: %v\n%s", rerr, out)
	}
	if _, serr := os.Lstat(preRestoreName(target)); serr == nil {
		t.Errorf("the pre-restore file is still at %s after the printed command ran", preRestoreName(target))
	}
	if got := keyNames(t, target); !slices.Equal(got, []string{"beta", "gamma"}) {
		t.Errorf("after the printed command the database holds %v, want [beta gamma]", got)
	}
}

// TestShellQuoteIsOneWord checks the quoting rule on its own: each quote written out, and the
// shell reads back the string it was given.
func TestShellQuoteIsOneWord(t *testing.T) {
	for _, in := range []string{"plain", "with space", "it's", "''", `back\slash`, "$HOME `x`"} {
		out, err := exec.Command("sh", "-c", "printf %s "+shellQuote(in)).Output()
		if err != nil {
			t.Fatalf("sh could not read %q: %v", shellQuote(in), err)
		}
		if string(out) != in {
			t.Errorf("shellQuote(%q) read back as %q", in, out)
		}
	}
}

// TestSQLiteRefusalNamesTheNextStep checks 14b-S2: CheckReadable passed and the lock then finds the
// file is not a database. The refusal says nothing was moved or deleted, and gives the next step.
// The seam stands for the race between the two opens.
func TestSQLiteRefusalNamesTheNextStep(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")

	prev := acquireExclusive
	acquireExclusive = func(context.Context, string) (*store.ExclusiveLock, error) {
		return nil, fmt.Errorf("store: lock %s: %w", target, store.ErrNotDatabase)
	}
	t.Cleanup(func() { acquireExclusive = prev })

	opts, _ := restoreRun(target, backup, target+"\n", true)
	err := Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "could not be locked as a database") ||
		!strings.Contains(err.Error(), "nothing was moved or deleted") || !strings.Contains(err.Error(), "integrity_check") {
		t.Fatalf("Restore when the lock finds no database: err = %v, want the lock refusal with a next step", err)
	}
	assertUntouched(t, target, "beta")
}

// TestCheckFailureNamesTheJournal checks 14b-S3: a read-only check fails and a -journal sits beside
// the database. The refusal names the journal and the step that rolls it back. Without a journal
// the same failure gets no such advice.
func TestCheckFailureNamesTheJournal(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")
	if err := os.WriteFile(target+"-journal", []byte("hot"), 0o600); err != nil {
		t.Fatal(err)
	}

	prev := checkReadable
	checkReadable = func(context.Context, string) error {
		return errors.New("store: lock " + target + ": attempt to write a readonly database")
	}
	t.Cleanup(func() { checkReadable = prev })

	opts, _ := restoreRun(target, backup, target+"\n", true)
	err := Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "cannot roll it back") || !strings.Contains(err.Error(), "sqlite3") ||
		!strings.Contains(err.Error(), "nothing was moved or deleted") {
		t.Fatalf("Restore with a hot journal: err = %v, want the journal refusal with a next step", err)
	}
	// The refusal is made before anything moves. The check is a seam here, so the journal is left
	// as it was written; no store is opened on the path, since an open would delete a journal that
	// is not hot.
	if _, serr := os.Lstat(target + "-journal"); serr != nil {
		t.Errorf("the journal beside the database was changed by a refused restore: %v", serr)
	}
	if _, serr := os.Lstat(preRestoreName(target)); serr == nil {
		t.Errorf("a refused restore created %s", preRestoreName(target))
	}

	if err := os.Remove(target + "-journal"); err != nil {
		t.Fatal(err)
	}
	opts, _ = restoreRun(target, backup, target+"\n", true)
	err = Restore(context.Background(), opts)
	if err == nil || strings.Contains(err.Error(), "sqlite3") {
		t.Fatalf("Restore with no journal: err = %v, want the plain check failure with no journal advice", err)
	}
}
