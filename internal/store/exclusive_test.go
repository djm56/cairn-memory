package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// stashWAL makes the database at path hold a -wal with a row that is in no other file: the file
// set is copied while a connection with checkpointing off has committed one row, and the copy is
// put back after that connection closes. The row is "wal-only". It returns nothing; the caller
// reads the file set through a raw connection.
func stashWAL(t *testing.T, path string) {
	t.Helper()
	conn, err := sql.Open("sqlite", path+"?_busy_timeout=5000&_foreign_keys=1")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec(`PRAGMA wal_autocheckpoint = 0`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO api_keys (name, hash, scope, created_at) VALUES ('wal-only', 'hash-wal-only', 'agent', '2026-10-31T00:00:00.000000000Z')`); err != nil {
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

// rawNames reads the api_keys names of the file at path with a plain connection, which sets no
// journal mode and runs no migration.
func rawNames(t *testing.T, path string) []string {
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

// dirNames lists the entries of dir, sorted.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// TestConsolidateFoldsTheWALAndLeavesTheMainFileWhole checks the step the restore runs under the
// lock before any move: the -wal's rows end up in the main file, the -wal is gone, and the database
// is out of WAL mode, so the main file alone reads as the whole state.
func TestConsolidateFoldsTheWALAndLeavesTheMainFileWhole(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cairn.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	stashWAL(t, path)
	if _, err := os.Stat(path + "-wal"); err != nil {
		t.Fatalf("setup: no -wal beside the database: %v", err)
	}

	lock, err := AcquireExclusive(ctx, path)
	if err != nil {
		t.Fatalf("AcquireExclusive: %v", err)
	}
	if err := lock.Consolidate(ctx); err != nil {
		t.Fatalf("Consolidate: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// The -wal is removed by the switch to DELETE. The -shm the stash left is not, so it stays
	// beside the database; the restore moves it with the database and reports it.
	if got := dirNames(t, filepath.Dir(path)); !slices.Equal(got, []string{"cairn.db", "cairn.db-shm"}) {
		t.Errorf("after Consolidate and Release the directory holds %v, want [cairn.db cairn.db-shm]", got)
	}
	if got := rawNames(t, path); !slices.Contains(got, "wal-only") {
		t.Errorf("the main file holds %v after Consolidate: the -wal row is not in it", got)
	}
}

// TestCheckReadableLeavesTheWALOfAFileItCannotRead checks the proof the restore's safety rests on:
// a file that cannot be read as a database is reported ErrNotDatabase, and its -wal is still there
// afterwards. A read-write open of the same file deletes the -wal on close, which is why the check
// is read-only.
func TestCheckReadableLeavesTheWALOfAFileItCannotRead(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "cairn.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	copy(data[:16], []byte("XXXXXXXXXXXXXXXX"))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	const walBytes = "a -wal that belongs to nothing readable"
	if err := os.WriteFile(path+"-wal", []byte(walBytes), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := CheckReadable(ctx, path); !errors.Is(err, ErrNotDatabase) {
		t.Fatalf("CheckReadable of an unreadable file: err = %v, want ErrNotDatabase", err)
	}
	got, err := os.ReadFile(path + "-wal")
	if err != nil || string(got) != walBytes {
		t.Fatalf("the -wal of an unreadable file after CheckReadable: %q (%v), want it unchanged", got, err)
	}
}

// TestCheckReadableAcceptsADatabaseAndKeepsItsWAL checks that a readable database passes the check,
// and that the check does not take its -wal: the row in the -wal is still there after it.
func TestCheckReadableAcceptsADatabaseAndKeepsItsWAL(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cairn.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	stashWAL(t, path)
	if err := CheckReadable(ctx, path); err != nil {
		t.Fatalf("CheckReadable of a readable database: %v", err)
	}
	if _, err := os.Stat(path + "-wal"); err != nil {
		t.Errorf("the check removed the -wal of a readable database: %v", err)
	}
}

// TestAcquireExclusiveRefusesAWriterRunning checks the probe while a server is writing, not only
// when it is idle: every attempt during the writes is ErrInUse. The writer runs one transaction
// after another against an open store, which is what a busy server does.
func TestAcquireExclusiveRefusesAWriterRunning(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cairn.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var writerErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			name := fmt.Sprintf("writer-%d", i)
			err := st.WithTx(ctx, func(q Querier) error {
				_, err := AddAPIKey(ctx, q, APIKeyNew{Name: name, Hash: "hash-" + name, Scope: "agent", CreatedAt: time.Now().UTC()})
				return err
			})
			if err != nil {
				writerErr = err
				return
			}
		}
	}()

	for i := 0; i < 20; i++ {
		lock, err := AcquireExclusive(ctx, path)
		if !errors.Is(err, ErrInUse) {
			if lock != nil {
				_ = lock.Release()
			}
			close(stop)
			wg.Wait()
			t.Fatalf("attempt %d with a writer running: err = %v, want ErrInUse", i, err)
		}
	}
	close(stop)
	wg.Wait()
	if writerErr != nil {
		t.Fatalf("the writer failed: %v", writerErr)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireExclusive(ctx, path)
	if err != nil {
		t.Fatalf("AcquireExclusive after the writer and the store closed: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}

// TestOpenExistingLeavesARefusedFileAsItWas checks 14-S5: a file with no Cairn schema is refused
// without being switched to WAL. Its header bytes that say the journal mode are the same afterwards,
// and no -wal is created beside it.
func TestOpenExistingLeavesARefusedFileAsItWas(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.db")
	raw, err := sql.Open("sqlite", plain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	before, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	if before[18] != 1 || before[19] != 1 {
		t.Fatalf("setup: the file is not in the rollback journal mode (bytes 18,19 = %d,%d)", before[18], before[19])
	}

	if _, err := OpenExisting(plain); err == nil {
		t.Fatal("OpenExisting of a file with no schema: want a refusal")
	}
	after, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	if after[18] != 1 || after[19] != 1 {
		t.Errorf("a refused file was switched to WAL: bytes 18,19 = %d,%d", after[18], after[19])
	}
	if got := dirNames(t, dir); !slices.Equal(got, []string{"plain.db"}) {
		t.Errorf("the directory after a refused open = %v, want the file alone", got)
	}
}
