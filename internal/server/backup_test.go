package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/store"
)

// createDB makes the database at path with the store, and closes it. It holds no rows.
func createDB(t *testing.T, path string) {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
}

// addKeyNamed writes one agent key named name into st. The hash is a placeholder: the key
// table only needs it to be present, and these tests never authenticate with it.
func addKeyNamed(t *testing.T, st *store.Store, name string) {
	t.Helper()
	err := st.WithTx(context.Background(), func(q store.Querier) error {
		_, err := store.AddAPIKey(context.Background(), q, store.APIKeyNew{
			Name: name, Hash: "hash-" + name, Scope: "agent", CreatedAt: time.Now().UTC(),
		})
		return err
	})
	if err != nil {
		t.Fatalf("add key %s: %v", name, err)
	}
}

// keyNames returns the names of every key in the database at path, opened without migrating.
func keyNames(t *testing.T, path string) []string {
	t.Helper()
	st, err := store.OpenExisting(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer st.Close()
	var out []string
	err = st.WithReadTx(context.Background(), func(q store.Querier) error {
		keys, err := store.ListAPIKeys(context.Background(), q)
		for _, k := range keys {
			out = append(out, k.Name)
		}
		return err
	})
	if err != nil {
		t.Fatalf("list keys in %s: %v", path, err)
	}
	return out
}

// assertIntegrityOK opens the database at path and fails the test unless PRAGMA integrity_check
// passes.
func assertIntegrityOK(t *testing.T, path string) {
	t.Helper()
	st, err := store.OpenExisting(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer st.Close()
	if err := st.IntegrityCheck(context.Background()); err != nil {
		t.Fatalf("integrity of %s: %v", path, err)
	}
}

// TestBackupWhileWritesRun is the consistency proof for cairn backup (#74). Writes run in another goroutine
// against the same database while Backup runs. The backup must open, pass the integrity
// check, and hold every row that was committed before Backup started.
func TestBackupWhileWritesRun(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cairn.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	const txRows = 3
	var committed atomic.Int64
	stop := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				writerDone <- nil
				return
			default:
			}
			// Each transaction writes txRows rows that belong together. A copy holding part of one
			// would show a group of fewer than txRows, which the check below refuses.
			err := st.WithTx(context.Background(), func(q store.Querier) error {
				for j := 0; j < txRows; j++ {
					name := fmt.Sprintf("w%06d-%d", i, j)
					if _, err := store.AddAPIKey(context.Background(), q, store.APIKeyNew{
						Name: name, Hash: "hash-" + name, Scope: "agent", CreatedAt: time.Now().UTC(),
					}); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				writerDone <- err
				return
			}
			committed.Add(1)
		}
	}()

	// Wait until the writer has committed enough rows to make the test mean something.
	deadline := time.Now().Add(20 * time.Second)
	for committed.Load() < 50 {
		if time.Now().After(deadline) {
			close(stop)
			t.Fatalf("the writer committed %d rows in 20s, want 50", committed.Load())
		}
		time.Sleep(time.Millisecond)
	}
	before := committed.Load()

	out := filepath.Join(dir, "backup.db")
	size, err := Backup(context.Background(), db, out)
	close(stop)
	if werr := <-writerDone; werr != nil {
		t.Fatalf("the writer failed during the backup: %v", werr)
	}
	if err != nil {
		t.Fatalf("Backup while writes run: %v", err)
	}
	if info, serr := os.Stat(out); serr != nil || info.Size() != size {
		t.Fatalf("Backup reported %d bytes; the file is %v (%v)", size, info, serr)
	}

	assertIntegrityOK(t, out)
	// Group the writer's rows by transaction. Each group must be whole in the copy: all txRows
	// rows or none, never a part (14-S9). Every transaction committed before Backup began must be
	// in the copy.
	groups := map[string]int{}
	for _, name := range keyNames(t, out) {
		if strings.HasPrefix(name, "w") {
			groups[name[:strings.LastIndex(name, "-")]]++
		}
	}
	for tx, n := range groups {
		if n != txRows {
			t.Errorf("transaction %s is partly in the backup: %d of %d rows", tx, n, txRows)
		}
	}
	if int64(len(groups)) < before {
		t.Errorf("the backup holds %d writer transactions, want at least the %d committed before it started", len(groups), before)
	}
}

// TestBackupWritesANewOwnerOnlyFile checks that the copy is a new file with dbFileMode, and that
// an existing file is refused and left as it was.
func TestBackupWritesANewOwnerOnlyFile(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cairn.db")
	createDB(t, db)
	out := filepath.Join(dir, "backup.db")

	if _, err := Backup(context.Background(), db, out); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != dbFileMode {
		t.Errorf("backup mode = %04o, want %04o: the copy holds every key's hash", mode, dbFileMode)
	}

	before, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(context.Background(), db, out); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("a second backup to %s: err = %v, want the already-exists refusal", out, err)
	}
	after, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("a refused backup changed the existing file")
	}
}

// TestBackupRefusesWhatCannotBeAFile checks the output names the server would refuse for its own
// database, and a missing database, and that none of them leaves a file.
func TestBackupRefusesWhatCannotBeAFile(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cairn.db")
	createDB(t, db)
	cases := []struct {
		name, out, want string
	}{
		{"empty", "", "needs an output file"},
		{"memory", ":memory:", "memory database"},
		{"file uri", "file:backup.db", "memory database or a file: URI"},
		{"query", "backup.db?mode=rw", "must not contain '?'"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Backup(context.Background(), db, c.out); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("Backup to %q: err = %v, want it to contain %q", c.out, err, c.want)
			}
		})
	}

	missing := filepath.Join(dir, "absent.db")
	out := filepath.Join(dir, "from-absent.db")
	if _, err := Backup(context.Background(), missing, out); err == nil || !strings.Contains(err.Error(), "no database at") {
		t.Errorf("Backup of a missing database: err = %v, want no database at", err)
	}
	if _, err := os.Lstat(missing); !os.IsNotExist(err) {
		t.Errorf("a backup of a missing database created %s", missing)
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Errorf("a backup of a missing database created the output %s", out)
	}
}

// TestBackupRefusesAFileThatIsNotACairnDatabase checks that a file with no schema is refused, and
// that the output is not left behind.
func TestBackupRefusesAFileThatIsNotACairnDatabase(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "other.db")
	if err := os.WriteFile(db, []byte("not a database at all, just text padding it out to a page of bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "backup.db")
	if _, err := Backup(context.Background(), db, out); err == nil {
		t.Fatalf("a backup of a file that is not Cairn's: want a refusal")
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Errorf("a refused backup left %s behind", out)
	}
}
