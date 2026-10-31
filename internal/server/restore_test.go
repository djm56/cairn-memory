package server

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/store"
)

// restoreNow is the clock the restore tests give Restore, so the pre-restore name is known.
var restoreNow = time.Date(2026, 10, 31, 10, 11, 12, 0, time.UTC)

// preRestoreName is the name the database at db moves to in a restore run at restoreNow.
func preRestoreName(db string) string {
	return db + ".pre-restore-20261031T101112Z"
}

// seedDB makes the database at path with one key per name, and closes it.
func seedDB(t *testing.T, path string, names ...string) {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer st.Close()
	for _, name := range names {
		addKeyNamed(t, st, name)
	}
}

// restoreRun is what a test passes to Restore: the database, the backup, the typed answer, and
// whether stdin is a terminal. It returns the options and the buffer the prompt goes to.
func restoreRun(db, backup, typed string, terminal bool) (RestoreOptions, *bytes.Buffer) {
	var out bytes.Buffer
	return RestoreOptions{
		DB: db, Backup: backup, Terminal: terminal,
		Input: strings.NewReader(typed), Out: &out,
		Now: func() time.Time { return restoreNow },
	}, &out
}

// assertUntouched fails the test unless the database at db still holds exactly the keys names,
// and no pre-restore file was created: a refused restore changes nothing.
func assertUntouched(t *testing.T, db string, names ...string) {
	t.Helper()
	assertKeys(t, db, names...)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if _, err := os.Lstat(preRestoreName(db) + suffix); err == nil {
			t.Errorf("a refused restore created %s", preRestoreName(db)+suffix)
		}
	}
}

// assertKeys fails the test unless the database at db holds exactly the keys names.
func assertKeys(t *testing.T, db string, names ...string) {
	t.Helper()
	if got := keyNames(t, db); !slices.Equal(got, names) {
		t.Errorf("the database holds %v, want %v", got, names)
	}
}

// leaveWAL makes the database at path hold a -wal and a -shm the way a server that stopped
// without a checkpoint leaves them: the file set is copied while a connection with checkpointing
// off has committed one more row, and the copy is put back after that connection closes. The
// row is in the -wal only, so the main file alone does not hold it. It returns the row's name.
func leaveWAL(t *testing.T, path, name string) {
	t.Helper()
	conn, err := sql.Open("sqlite", path+"?_busy_timeout=5000&_foreign_keys=1")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(`PRAGMA wal_autocheckpoint = 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO api_keys (name, hash, scope, created_at) VALUES (?, ?, 'agent', '2026-10-31T00:00:00.000000000Z')`, name, "hash-"+name); err != nil {
		t.Fatal(err)
	}
	stash := t.TempDir()
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(path + suffix)
		if err != nil {
			t.Fatalf("stash %s: %v", path+suffix, err)
		}
		if err := os.WriteFile(filepath.Join(stash, filepath.Base(path)+suffix), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		data, err := os.ReadFile(filepath.Join(stash, filepath.Base(path)+suffix))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+suffix, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// makeBackup seeds a database at dir with names, and backs it up. It returns the backup's path.
func makeBackup(t *testing.T, dir string, names ...string) string {
	t.Helper()
	src := filepath.Join(dir, "source.db")
	seedDB(t, src, names...)
	backup := filepath.Join(dir, "backup.db")
	if _, err := Backup(context.Background(), src, backup); err != nil {
		t.Fatalf("make the backup: %v", err)
	}
	return backup
}

// rawSQL runs statements on the file at path through database/sql, for the tests that need to
// make a file the store would not. The store is not used, so no migration runs.
func rawSQL(t *testing.T, path string, stmts ...string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// TestRestoreRefusesANonTerminal checks step 3's first rule: a restore is confirmed at a terminal,
// so with stdin not one, the typed answer is not even read, and the database is untouched.
func TestRestoreRefusesANonTerminal(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")

	opts, _ := restoreRun(target, backup, target+"\n", false)
	err := Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "not a terminal") {
		t.Fatalf("Restore with stdin not a terminal: err = %v, want the not-a-terminal refusal", err)
	}
	assertUntouched(t, target, "beta")
}

// TestRestoreRefusesAWrongTypedPath checks that the operator must type the path exactly.
func TestRestoreRefusesAWrongTypedPath(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")

	for _, typed := range []string{"", "cairn.db\n", target + " \n", filepath.Join(dir, "other.db") + "\n", "\n"} {
		opts, _ := restoreRun(target, backup, typed, true)
		if err := Restore(context.Background(), opts); err == nil {
			t.Fatalf("Restore with the typed answer %q: want a refusal", typed)
		}
		assertUntouched(t, target, "beta")
	}
	// The exact path, with its line ending, goes through.
	opts, _ := restoreRun(target, backup, target+"\r\n", true)
	if err := Restore(context.Background(), opts); err != nil {
		t.Fatalf("Restore with the exact path typed: %v", err)
	}
}

// TestRestoreRefusesWhileTheDatabaseIsLocked checks step 2: a database another connection holds is
// not replaced, even when the other connection is idle, as an open server's is between calls.
func TestRestoreRefusesWhileTheDatabaseIsLocked(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")

	held, err := store.Open(target)
	if err != nil {
		t.Fatalf("open the database as a server does: %v", err)
	}
	defer held.Close()

	opts, _ := restoreRun(target, backup, target+"\n", true)
	err = Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("Restore while the database is open: err = %v, want the in-use refusal", err)
	}
	// The server's connection still works, and nothing moved.
	if got := keyNames(t, target); !slices.Equal(got, []string{"beta"}) {
		t.Errorf("the open database holds %v after a refused restore, want [beta]", got)
	}
	assertUntouched(t, target, "beta")
}

// TestRestoreRefusesACorruptBackup checks step 1: a backup that fails PRAGMA integrity_check is not
// restored. The copy is made corrupt by zeroing the page of one index of a real backup, which
// leaves the schema readable and the integrity check failing.
func TestRestoreRefusesACorruptBackup(t *testing.T) {
	dir := t.TempDir()
	good := makeBackup(t, dir, "alpha", "gamma")
	corrupt := filepath.Join(dir, "corrupt.db")
	data, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(corrupt, data, 0o600); err != nil {
		t.Fatal(err)
	}
	zeroIndexPage(t, corrupt)
	st, err := store.OpenExisting(corrupt)
	if err == nil {
		ierr := st.IntegrityCheck(context.Background())
		st.Close()
		if ierr == nil {
			t.Fatalf("the corruption did not take: the integrity check passes on %s", corrupt)
		}
	}

	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")
	opts, _ := restoreRun(target, corrupt, target+"\n", true)
	err = Restore(context.Background(), opts)
	if err == nil || (!strings.Contains(err.Error(), "corrupt") && !strings.Contains(err.Error(), "not usable")) {
		t.Fatalf("Restore of a corrupt backup: err = %v, want a refusal that says it is corrupt", err)
	}
	assertUntouched(t, target, "beta")
}

// zeroIndexPage overwrites, with zeros, the first index page of the database at path. The header
// and the schema page are left as they are, so the file still opens.
func zeroIndexPage(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var pageSize, root int
	if err := db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT rootpage FROM sqlite_master WHERE type = 'index' AND rootpage > 1 ORDER BY rootpage LIMIT 1`).Scan(&root); err != nil {
		t.Fatalf("no index to corrupt: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteAt(make([]byte, pageSize), int64(root-1)*int64(pageSize)); err != nil {
		t.Fatal(err)
	}
}

// TestRestoreRefusesANewerSchemaBackup checks step 1's version rule: a backup from a newer build
// is refused, not migrated down.
func TestRestoreRefusesANewerSchemaBackup(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	future := filepath.Join(dir, "future.db")
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(future, data, 0o600); err != nil {
		t.Fatal(err)
	}
	rawSQL(t, future, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (99, 'from_a_newer_build', '2030-01-01T00:00:00Z')`)

	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")
	opts, _ := restoreRun(target, future, target+"\n", true)
	err = Restore(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "newer than this build") {
		t.Fatalf("Restore of a newer-schema backup: err = %v, want the newer-than-this-build refusal", err)
	}
	assertUntouched(t, target, "beta")
}

// TestRestoreRefusesABackupThatIsTheDatabase checks that a restore cannot be run from the database
// it would replace, which would move the copy away with it.
func TestRestoreRefusesABackupThatIsTheDatabase(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")
	opts, _ := restoreRun(target, target, target+"\n", true)
	if err := Restore(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "is the database itself") {
		t.Fatalf("Restore from the database itself: err = %v, want the refusal", err)
	}
	assertUntouched(t, target, "beta")
}

// TestRestoreRefusesATakenPreRestoreName checks that a restore never writes over a file it would
// move aside: a pre-restore name that exists is refused before anything moves.
func TestRestoreRefusesATakenPreRestoreName(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")
	if err := os.WriteFile(preRestoreName(target), []byte("kept"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, _ := restoreRun(target, backup, target+"\n", true)
	if err := Restore(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "is taken") {
		t.Fatalf("Restore with a taken pre-restore name: err = %v, want the taken refusal", err)
	}
	assertKeys(t, target, "beta")
	if b, _ := os.ReadFile(preRestoreName(target)); string(b) != "kept" {
		t.Errorf("the file at the pre-restore name changed")
	}
}

// TestRestoreReplacesAndKeepsTheOldFiles is the success case. The old database and its -wal and
// -shm are moved to the pre-restore names, none is deleted, the restored data reads back, and the
// restored file is owner-only.
func TestRestoreReplacesAndKeepsTheOldFiles(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "cairn.db")
	seedDB(t, target, "beta")
	// The database as a server that stopped leaves it: "gamma" is in its -wal, not its main file.
	leaveWAL(t, target, "gamma")

	opts, out := restoreRun(target, backup, target+"\n", true)
	if err := Restore(context.Background(), opts); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if got := keyNames(t, target); !slices.Equal(got, []string{"alpha"}) {
		t.Errorf("restored database holds %v, want [alpha]", got)
	}
	// The kept database holds what was in its -wal too: beta and gamma, as the server last saw them.
	if got := keyNames(t, preRestoreName(target)); !slices.Equal(got, []string{"beta", "gamma"}) {
		t.Errorf("the pre-restore database holds %v, want [beta gamma]: the -wal was not kept with it", got)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(target + suffix); err == nil {
			t.Errorf("%s is still at the database name after the restore", target+suffix)
		}
	}
	// The -wal and -shm are not kept as files. SQLite folds the -wal into the database it is
	// closing and removes both on its clean close. The state they held is in the kept database,
	// as the keys above show. Only the database file is asserted kept.
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != dbFileMode {
		t.Errorf("restored database mode = %04o, want %04o", mode, dbFileMode)
	}
	for _, want := range []string{preRestoreName(target), "restored " + target} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the report %q does not name %q", out.String(), want)
		}
	}
}

// TestRestoreIntoAnAbsentDatabase checks a restore to a path with no database: nothing is moved,
// and the backup is in place.
func TestRestoreIntoAnAbsentDatabase(t *testing.T) {
	dir := t.TempDir()
	backup := makeBackup(t, dir, "alpha")
	target := filepath.Join(dir, "fresh.db")
	opts, out := restoreRun(target, backup, target+"\n", true)
	if err := Restore(context.Background(), opts); err != nil {
		t.Fatalf("Restore into an absent database: %v", err)
	}
	if got := keyNames(t, target); !slices.Equal(got, []string{"alpha"}) {
		t.Errorf("restored database holds %v, want [alpha]", got)
	}
	if !strings.Contains(out.String(), "no database was at") {
		t.Errorf("the report %q does not say nothing was moved", out.String())
	}
	if _, err := os.Lstat(preRestoreName(target)); err == nil {
		t.Errorf("a restore into an absent database made %s", preRestoreName(target))
	}
}
