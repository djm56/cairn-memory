package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/store"
)

// listDir returns the names of the entries in dir, sorted. It is the directory listing the restore
// tests assert on: exactly which files exist, under which names.
func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("list %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// assertReportNamesMoves fails unless the report names, as a move, every file beside db that a
// restore moved to a pre-restore name: each one is reported as "moved <from> to <to>".
func assertReportNamesMoves(t *testing.T, db, report string) {
	t.Helper()
	dir := filepath.Dir(db)
	base := filepath.Base(preRestoreName(db))
	moved := 0
	for _, name := range listDir(t, dir) {
		if !strings.HasPrefix(name, base) {
			continue
		}
		moved++
		suffix := strings.TrimPrefix(name, base)
		want := fmt.Sprintf("moved %s to %s\n", db+suffix, filepath.Join(dir, name))
		if !strings.Contains(report, want) {
			t.Errorf("the report %q does not name the move %q", report, strings.TrimSpace(want))
		}
	}
	if moved == 0 {
		t.Errorf("no pre-restore file beside %s, so nothing was moved to check the report against", db)
	}
}

// rawKeyNames reads the api_keys names of the SQLite file at path with a plain connection: no
// journal mode is set and no migration runs, so the file is read as it is on disk. A -wal beside
// it would be replayed, so a test that wants the main file alone copies it somewhere with none.
func rawKeyNames(t *testing.T, path string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT name FROM api_keys ORDER BY name`)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// readBytes returns the file at path, or fails the test.
func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// TestRestoreFoldsTheWALIntoTheKeptDatabase is the proof for 14-C1. The database has a row that
// lives only in its -wal ("gamma"). After the restore, the pre-restore main file on its own, copied
// to a new directory with no -wal beside it, holds that row. The directory listing shows exactly
// the files that exist, and the report names every file moved.
//
// The -wal is kept out of the main file's way by leaveWAL: the writing connection has
// wal_autocheckpoint off, and the file set is copied while it is open, so the copy holds the row
// in the -wal only. A reader that opened the database would fold it in, so the proof reads the
// copy without one.
func TestRestoreFoldsTheWALIntoTheKeptDatabase(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")
	leaveWAL(t, target, "gamma")
	if before := listDir(t, dir); !slices.Contains(before, "cairn.db-wal") {
		t.Fatalf("setup: no -wal beside the database before the restore: %v", before)
	}

	opts, out := restoreRun(target, backup, target+"\n", true)
	if err := Restore(context.Background(), opts); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// The -wal is folded in and removed by the checkpoint and the switch to DELETE. The -shm is not
	// removed by either, so it is moved with the database and reported like any other file of the
	// set. Nothing else beside the database is left at its name.
	want := []string{
		"backup.db", "cairn.db", "cairn.db.pre-restore-20261031T101112Z",
		"cairn.db.pre-restore-20261031T101112Z-shm", "source.db",
	}
	got := listDir(t, dir)
	t.Logf("directory after the restore: %v", got)
	if !slices.Equal(got, want) {
		t.Errorf("directory after the restore = %v, want %v", got, want)
	}
	t.Logf("report:\n%s", out.String())
	assertReportNamesMoves(t, target, out.String())

	alone := t.TempDir()
	kept := filepath.Join(alone, "cairn.db")
	if err := os.WriteFile(kept, readBytes(t, preRestoreName(target)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := rawKeyNames(t, kept); !slices.Equal(got, []string{"beta", "gamma"}) {
		t.Errorf("the pre-restore main file alone holds %v, want [beta gamma]: the -wal row is not in it", got)
	}
	if got := listDir(t, alone); !slices.Equal(got, []string{"cairn.db"}) {
		t.Errorf("the copy directory = %v, want only the main file", got)
	}
}

// TestRestoreRefusesAMissingBackup checks step 1's first refusal: a backup path with no file.
func TestRestoreRefusesAMissingBackup(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")
	before := listDir(t, dir)
	opts, _ := restoreRun(target, filepath.Join(dir, "nope.db"), target+"\n", true)
	err := Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "cannot read the backup") {
		t.Fatalf("Restore of a missing backup: err = %v, want the cannot-read refusal", err)
	}
	assertUntouched(t, target, "beta")
	if after := listDir(t, dir); !slices.Equal(after, before) {
		t.Errorf("directory changed by a refused restore: %v, was %v", after, before)
	}
}

// TestRestoreRefusesABackupThatIsNotARegularFile checks that a directory given as the backup is
// refused, not read.
func TestRestoreRefusesABackupThatIsNotARegularFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")
	opts, _ := restoreRun(target, dir, target+"\n", true)
	err := Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "is not a regular file") {
		t.Fatalf("Restore from a directory: err = %v, want the not-a-regular-file refusal", err)
	}
	assertUntouched(t, target, "beta")
}

// TestRestoreRefusesATakenWALOrSHMName checks that a pre-restore -wal or -shm name already in use
// refuses the restore before anything moves, as the main name does. The keys are checked by name
// only: reading a database with a -wal beside it folds the -wal in on the last close, so the
// directory is not compared.
func TestRestoreRefusesATakenWALOrSHMName(t *testing.T) {
	for _, suffix := range []string{"-wal", "-shm"} {
		t.Run(suffix, func(t *testing.T) {
			dir := t.TempDir()
			backup := makeBackup(t, dir, "alpha")
			target := filepath.Join(dir, "cairn.db")
			seedDB(t, target, "beta")
			leaveWAL(t, target, "gamma")
			if err := os.WriteFile(preRestoreName(target)+suffix, []byte("kept"), 0o600); err != nil {
				t.Fatal(err)
			}
			opts, _ := restoreRun(target, backup, target+"\n", true)
			err := Restore(context.Background(), opts)
			if err == nil || !strings.Contains(err.Error(), "is taken") {
				t.Fatalf("Restore with a taken %s name: err = %v, want the taken refusal", suffix, err)
			}
			if got := string(readBytes(t, preRestoreName(target)+suffix)); got != "kept" {
				t.Errorf("the file at the taken %s name changed", suffix)
			}
			if _, err := os.Lstat(preRestoreName(target)); err == nil {
				t.Errorf("a refused restore moved the database to %s", preRestoreName(target))
			}
			assertKeys(t, target, "beta", "gamma")
		})
	}
}

// TestRestoreFailureBetweenMoveAndPlaceNamesTheOldDatabase is the proof for the place step. The
// rename that puts the checked copy in place fails after the database has moved. The error names
// the pre-restore path and the recovery step, the database before the restore is still whole at
// that path, and the recovery step the error gives works.
func TestRestoreFailureBetweenMoveAndPlaceNamesTheOldDatabase(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")
	leaveWAL(t, target, "gamma")

	prev := placeCopy
	placeCopy = func(string, string) error { return errors.New("injected place failure") }
	t.Cleanup(func() { placeCopy = prev })

	opts, out := restoreRun(target, backup, target+"\n", true)
	err := Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), preRestoreName(target)) || !strings.Contains(err.Error(), "mv ") {
		t.Fatalf("Restore with the place step failing: err = %v, want the pre-restore path and a recovery step", err)
	}
	assertReportNamesMoves(t, target, out.String())
	if _, err := os.Lstat(target); err == nil {
		t.Errorf("a database is at %s after the place step failed", target)
	}
	if got := rawKeyNames(t, preRestoreName(target)); !slices.Equal(got, []string{"beta", "gamma"}) {
		t.Errorf("the database before the restore holds %v at the pre-restore path, want [beta gamma]", got)
	}
	// The recovery step the error names: move the file back. Then the database reads as before.
	if err := os.Rename(preRestoreName(target), target); err != nil {
		t.Fatalf("the recovery move: %v", err)
	}
	if got := keyNames(t, target); !slices.Equal(got, []string{"beta", "gamma"}) {
		t.Errorf("after the recovery move the database holds %v, want [beta gamma]", got)
	}
	if left := listDir(t, dir); slices.ContainsFunc(left, func(n string) bool { return strings.Contains(n, ".restore-") }) {
		t.Errorf("the staged copy was left behind: %v", left)
	}
}

// TestRestoreFailedOpenOfTheRestoredCopyNamesTheOldDatabase checks the migrate step: the restored
// copy does not open, and the error names the pre-restore path and the recovery step. The seam
// stands for a migration that fails.
func TestRestoreFailedOpenOfTheRestoredCopyNamesTheOldDatabase(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")

	prev := openRestored
	openRestored = func(string) (*store.Store, error) { return nil, errors.New("injected migrate failure") }
	t.Cleanup(func() { openRestored = prev })

	opts, _ := restoreRun(target, backup, target+"\n", true)
	err := Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "did not open") || !strings.Contains(err.Error(), preRestoreName(target)) {
		t.Fatalf("Restore with a failed open: err = %v, want the did-not-open refusal naming the pre-restore path", err)
	}
	if got := rawKeyNames(t, preRestoreName(target)); !slices.Equal(got, []string{"beta"}) {
		t.Errorf("the database before the restore holds %v at the pre-restore path, want [beta]", got)
	}
}

// TestRestoreFailedReleaseNamesTheOldDatabase checks the step after the moves: the probe's lock
// fails to release. The error names the pre-restore path and the recovery step. The seam releases
// the lock for real and then reports the failure, so nothing is left held.
func TestRestoreFailedReleaseNamesTheOldDatabase(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")

	prev := releaseLock
	releaseLock = func(l *store.ExclusiveLock) error {
		_ = l.Release()
		return errors.New("injected release failure")
	}
	t.Cleanup(func() { releaseLock = prev })

	opts, _ := restoreRun(target, backup, target+"\n", true)
	err := Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "release the lock") || !strings.Contains(err.Error(), preRestoreName(target)) {
		t.Fatalf("Restore with a failed release: err = %v, want the release refusal naming the pre-restore path", err)
	}
	if got := rawKeyNames(t, preRestoreName(target)); !slices.Equal(got, []string{"beta"}) {
		t.Errorf("the database before the restore holds %v at the pre-restore path, want [beta]", got)
	}
}

// TestRestoreRefusesASymbolicLinkDatabase checks 14-S2: a --db that is a link is refused, and the
// file it points to is not moved or touched.
func TestRestoreRefusesASymbolicLinkDatabase(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	real := filepath.Join(dir, "real.db")
	seedDB(t, real, "beta")
	link := filepath.Join(dir, "cairn.db")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	opts, _ := restoreRun(link, backup, link+"\n", true)
	err := Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Restore of a symlinked database: err = %v, want the symbolic-link refusal", err)
	}
	if fi, lerr := os.Lstat(link); lerr != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link at %s was changed", link)
	}
	assertKeys(t, real, "beta")
}

// TestRestoreMovesAFileThatIsNotADatabaseAsItIs covers 14-W2. A target that cannot be read as
// SQLite at all, from garbage or from a damaged header, is not refused. After the typed
// confirmation its files are moved aside as they are: no checkpoint, the bytes unchanged, every
// move reported, and the output says it was not a readable database.
func TestRestoreMovesAFileThatIsNotADatabaseAsItIs(t *testing.T) {
	cases := map[string]func(t *testing.T, path string){
		"garbage": func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte(strings.Repeat("this is not a database\n", 200)), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"damaged header": func(t *testing.T, path string) {
			seedDB(t, path, "beta")
			data := readBytes(t, path)
			copy(data[:16], []byte("XXXXXXXXXXXXXXXX"))
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, makeTarget := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			backup := makeBackup(t, dir, "alpha")
			target := filepath.Join(dir, "cairn.db")
			makeTarget(t, target)
			stray := []byte("a -wal that belongs to nothing readable")
			if err := os.WriteFile(target+"-wal", stray, 0o600); err != nil {
				t.Fatal(err)
			}
			origMain := readBytes(t, target)

			opts, out := restoreRun(target, backup, target+"\n", true)
			if err := Restore(context.Background(), opts); err != nil {
				t.Fatalf("Restore of an unreadable target: %v", err)
			}
			if !strings.Contains(out.String(), "is not a readable SQLite database") {
				t.Errorf("the report %q does not say the target was not a readable database", out.String())
			}
			// 14b-W1: the prompt says the lock could not be checked, and that serve must be stopped.
			if !strings.Contains(out.String(), "The lock could not be checked") || !strings.Contains(out.String(), "Stop cairn serve") {
				t.Errorf("the prompt %q does not say the lock could not be checked and to stop cairn serve", out.String())
			}
			assertReportNamesMoves(t, target, out.String())
			if got := readBytes(t, preRestoreName(target)); string(got) != string(origMain) {
				t.Errorf("the moved main file is not the bytes that were there: it was changed")
			}
			if got := readBytes(t, preRestoreName(target)+"-wal"); string(got) != string(stray) {
				t.Errorf("the moved -wal is not the bytes that were there: it was changed")
			}
			assertKeys(t, target, "alpha")
		})
	}
}

// TestRestoreMovesAStrayWALAsideFromAnAbsentPath is the proof for 14-W3. The database is absent,
// but a -wal from another database is beside its path. The restore moves that -wal aside under the
// pre-restore name, reports it, and the restored database does not replay it.
func TestRestoreMovesAStrayWALAsideFromAnAbsentPath(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	other := filepath.Join(dir, "other.db")
	seedDB(t, other, "delta")
	leaveWAL(t, other, "epsilon")
	stray := readBytes(t, other+"-wal")
	target := filepath.Join(dir, "cairn.db")
	if err := os.WriteFile(target+"-wal", stray, 0o600); err != nil {
		t.Fatal(err)
	}

	opts, out := restoreRun(target, backup, target+"\n", true)
	if err := Restore(context.Background(), opts); err != nil {
		t.Fatalf("Restore into an absent path with a stray -wal: %v", err)
	}
	if !strings.Contains(out.String(), "No database is at "+target) {
		t.Errorf("the prompt does not say no database is there: %q", out.String())
	}
	assertReportNamesMoves(t, target, out.String())
	if got := readBytes(t, preRestoreName(target)+"-wal"); string(got) != string(stray) {
		t.Errorf("the moved stray -wal is not the bytes that were there")
	}
	if _, err := os.Lstat(target + "-wal"); err == nil {
		t.Errorf("a -wal is still at %s after the restore", target+"-wal")
	}
	assertKeys(t, target, "alpha")
}

// TestRestoreRefusesANonTerminalBeforeTheLock checks 14-S1: a restore that cannot be confirmed
// reports that first, so a database held open by a server is not what the operator is told.
func TestRestoreRefusesANonTerminalBeforeTheLock(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")
	held, err := store.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	opts, _ := restoreRun(target, backup, target+"\n", false)
	err = Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "not a terminal") {
		t.Fatalf("Restore with stdin not a terminal and the database open: err = %v, want the not-a-terminal refusal first", err)
	}
}

// TestRestoreRemovesTheStagedCopyOnACorruptBackup covers 14-W1. A corrupt backup is refused in the
// staging step, and the directory afterwards holds exactly what it held before: no staged .tmp,
// .tmp-wal or .tmp-shm is left beside the database.
//
// The corrupt copy has the WAL flag in its header (bytes 18 and 19 set to 2), as a file copied
// with cp from a live server's database has. A VACUUM INTO copy never has it, so the flag is what
// makes staging create a -wal and -shm, and so what the cleanup has to remove.
func TestRestoreRemovesTheStagedCopyOnACorruptBackup(t *testing.T) {
	dir := t.TempDir()
	good := makeBackup(t, dir, "alpha", "gamma")
	corrupt := filepath.Join(dir, "corrupt.db")
	data := readBytes(t, good)
	data[18], data[19] = 2, 2
	if err := os.WriteFile(corrupt, data, 0o600); err != nil {
		t.Fatal(err)
	}
	zeroIndexPage(t, corrupt)
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")
	before := listDir(t, dir)

	opts, _ := restoreRun(target, corrupt, target+"\n", true)
	err := Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "corrupt") && !strings.Contains(err.Error(), "not usable") {
		t.Fatalf("Restore of a corrupt backup: err = %v, want a refusal that says it is corrupt", err)
	}
	if after := listDir(t, dir); !slices.Equal(after, before) {
		t.Errorf("the directory after the refusal = %v, want exactly %v: a staged file was left behind", after, before)
	}
}

// TestOpenDatabaseRefusesToCreateOverAnInterruptedRestore is the proof for the serve refusal. After
// a restore is interrupted between the move and the place, the path is empty and a pre-restore file
// is beside it. cairn serve, through Run, refuses to create an empty database there, names the file,
// and points at the recovery step.
func TestOpenDatabaseRefusesToCreateOverAnInterruptedRestore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cairn.db")
	pre := preRestoreName(path)
	if err := os.WriteFile(pre, []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A deadline stands in for a server that starts: if the refusal were missing, Run would serve
	// until the deadline and return nil, and the test would fail on its assertions, not hang.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := Run(ctx, Options{Addr: "127.0.0.1:0", DB: path, Log: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "a restore may have been interrupted") || !strings.Contains(err.Error(), pre) {
		t.Fatalf("Run over an interrupted restore: err = %v, want the interrupted-restore refusal naming %s", err, pre)
	}
	if _, serr := os.Lstat(path); serr == nil {
		t.Errorf("serve created %s over an interrupted restore", path)
	}
	if got := string(readBytes(t, pre)); got != "kept" {
		t.Errorf("the pre-restore file changed")
	}
}

// TestBackupRemovesAPartialCopy checks that a copy which fails part way leaves no file under the
// backup name. The seam writes some bytes and fails, as a full disk or a killed copy would.
func TestBackupRemovesAPartialCopy(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cairn.db")
	seedDB(t, db, "alpha")
	out := filepath.Join(dir, "backup.db")

	prev := copyBackup
	copyBackup = func(_ context.Context, _ *store.Store, path string) error {
		if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
		return errors.New("injected copy failure")
	}
	t.Cleanup(func() { copyBackup = prev })

	if _, err := Backup(context.Background(), db, out); err == nil {
		t.Fatal("Backup with a failing copy: want an error")
	}
	if _, err := os.Lstat(out); err == nil {
		t.Errorf("a partial copy is left at %s", out)
	}
}

// TestBackupRefusesASymbolicLinkOut checks that an output name that is a link, even a dangling one,
// is refused, and that the file it names is not created through the link.
func TestBackupRefusesASymbolicLinkOut(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cairn.db")
	seedDB(t, db, "alpha")
	elsewhere := filepath.Join(dir, "elsewhere.db")
	out := filepath.Join(dir, "backup.db")
	if err := os.Symlink(elsewhere, out); err != nil {
		t.Fatal(err)
	}
	_, err := Backup(context.Background(), db, out)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Backup to a symbolic link: err = %v, want the already-exists refusal", err)
	}
	if _, serr := os.Lstat(elsewhere); serr == nil {
		t.Errorf("the link target %s was created through the link", elsewhere)
	}
}
