package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
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
	// rows or none, never a part. Every transaction committed before Backup began must be
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

// TestBackupRefusesAStagingDirectorySwappedDuringTheCopy plays the attack that #129 closed:
// while the copy is made, the staging directory is renamed and a symbolic link is put at its
// name, pointing at a directory that holds a sentinel file. VACUUM INTO follows the link and
// writes the copy into that directory. The backup must be refused, the output must not exist,
// the sentinel must survive the cleanup, and the copy written into the swapped-in directory
// must be owner-only.
func TestBackupRefusesAStagingDirectorySwappedDuringTheCopy(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cairn.db")
	createDB(t, db)
	out := filepath.Join(dir, "backup.db")
	victim := filepath.Join(dir, "victim")
	if err := os.Mkdir(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(victim, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}

	prev := copyBackup
	copyBackup = func(ctx context.Context, st *store.Store, path string) error {
		stage := filepath.Dir(path)
		if err := os.Rename(stage, stage+".moved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(victim, stage); err != nil {
			t.Fatal(err)
		}
		return prev(ctx, st, path)
	}
	t.Cleanup(func() { copyBackup = prev })

	_, err := Backup(context.Background(), db, out)
	if err == nil || !strings.Contains(err.Error(), "was replaced") {
		t.Fatalf("Backup with the staging directory swapped during the copy: err = %v, want the replaced refusal", err)
	}
	if _, serr := os.Lstat(out); !os.IsNotExist(serr) {
		t.Errorf("a refused backup left the output %s behind", out)
	}
	got, rerr := os.ReadFile(sentinel)
	if rerr != nil || string(got) != "keep me" {
		t.Errorf("the sentinel in the swapped-in directory did not survive: %q, %v", got, rerr)
	}
	info, serr := os.Stat(filepath.Join(victim, copyName))
	if serr != nil {
		t.Fatalf("the copy VACUUM INTO wrote through the link is not where the test expects it: %v", serr)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("the copy written into the swapped-in directory has mode %04o, want no group or other bits", mode)
	}
}

// TestBackupRefusesAStagingDirectorySwappedAfterTheCopy checks a swap of the staging name after the copy. The
// copy is complete and sits in the directory the root holds when the staging name is moved and a
// link put in its place, pointing at a directory with a sentinel file. The check after the copy
// must refuse. The root-held copy must be removed through the root: it is removed from the moved
// directory, not by the name, which now holds the link. The sentinel must survive.
func TestBackupRefusesAStagingDirectorySwappedAfterTheCopy(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cairn.db")
	createDB(t, db)
	out := filepath.Join(dir, "backup.db")
	victim := filepath.Join(dir, "victim")
	if err := os.Mkdir(victim, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(victim, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}

	var moved string
	prev := afterCopy
	afterCopy = func(stage string) {
		if _, err := os.Stat(filepath.Join(stage, copyName)); err != nil {
			t.Fatalf("no copy in the staging directory before the swap: %v", err)
		}
		moved = stage + ".moved"
		if err := os.Rename(stage, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(victim, stage); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterCopy = prev })

	_, err := Backup(context.Background(), db, out)
	if err == nil || !strings.Contains(err.Error(), "was replaced") {
		t.Fatalf("Backup with the staging directory swapped after the copy: err = %v, want the replaced refusal", err)
	}
	if _, serr := os.Lstat(out); !os.IsNotExist(serr) {
		t.Errorf("a refused backup left the output %s behind", out)
	}
	got, rerr := os.ReadFile(sentinel)
	if rerr != nil || string(got) != "keep me" {
		t.Errorf("the sentinel in the swapped-in directory did not survive: %q, %v", got, rerr)
	}
	if _, serr := os.Lstat(filepath.Join(moved, copyName)); !os.IsNotExist(serr) {
		t.Errorf("the root-held copy in %s was not removed: %v", moved, serr)
	}
	if entries, rerr := os.ReadDir(moved); rerr != nil || len(entries) != 0 {
		t.Errorf("the moved staging directory %s holds %d entries after the refusal, want none (%v)", moved, len(entries), rerr)
	}
}

// TestBackupRemovesItsNameWhenTheCopyIsSwappedBeforeTheLink checks the post-link SameFile
// branch. The copy is replaced by another file between the last check and the link. The name is
// the same, so the check passes, and os.Link links the file that now holds it. The check on the
// linked file must find it is not the copy made, remove the output name, and say so.
func TestBackupRemovesItsNameWhenTheCopyIsSwappedBeforeTheLink(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cairn.db")
	createDB(t, db)
	out := filepath.Join(dir, "backup.db")

	var stage string
	prev := beforeLink
	beforeLink = func(tmp string) {
		stage = filepath.Dir(tmp)
		if err := os.Rename(tmp, tmp+".kept"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tmp, []byte("impostor"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeLink = prev })

	_, err := Backup(context.Background(), db, out)
	if err == nil || !strings.Contains(err.Error(), "is not the copy made") || !strings.Contains(err.Error(), "the name was removed") {
		t.Fatalf("Backup with the copy swapped before the link: err = %v, want the not-the-copy refusal that says the name was removed", err)
	}
	if _, serr := os.Lstat(out); !os.IsNotExist(serr) {
		t.Errorf("a refused backup left the output %s behind, linked to the impostor", out)
	}
	if _, serr := os.Lstat(stage); !os.IsNotExist(serr) {
		t.Errorf("the staging directory %s was not removed after the refusal: %v", stage, serr)
	}
}

// TestBackupRemovesItsNameWhenTheStagingDirectoryIsSwappedBeforeTheLink swaps the staging
// directory itself, not a file in it, between the last check and the link. The staging name is
// moved aside, and a directory holding an impostor copy and a sentinel is put in its place. The
// link takes the impostor's copy by name, the check on the linked file finds it is not the copy
// made, and the output name is removed. The refusal must also report that the staging directory was
// replaced, so the caller is told the directory made was not removed by name. The sentinel in the
// swapped-in directory must survive, and the directory made must be emptied through its root.
func TestBackupRemovesItsNameWhenTheStagingDirectoryIsSwappedBeforeTheLink(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cairn.db")
	createDB(t, db)
	out := filepath.Join(dir, "backup.db")

	var stage, moved string
	prev := beforeLink
	beforeLink = func(tmp string) {
		stage = filepath.Dir(tmp)
		moved = stage + ".moved"
		if err := os.Rename(stage, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(stage, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stage, copyName), []byte("impostor"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stage, "sentinel.txt"), []byte("keep me"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeLink = prev })

	_, err := Backup(context.Background(), db, out)
	if err == nil || !strings.Contains(err.Error(), "is not the copy made") || !strings.Contains(err.Error(), "the name was removed") {
		t.Fatalf("Backup with the staging directory swapped before the link: err = %v, want the not-the-copy refusal that says the name was removed", err)
	}
	if !strings.Contains(err.Error(), "was replaced") {
		t.Errorf("the refusal %q does not report that the staging directory was replaced", err)
	}
	if _, serr := os.Lstat(out); !os.IsNotExist(serr) {
		t.Errorf("a refused backup left the output %s behind, linked to the impostor", out)
	}
	got, rerr := os.ReadFile(filepath.Join(stage, "sentinel.txt"))
	if rerr != nil || string(got) != "keep me" {
		t.Errorf("the sentinel in the swapped-in directory did not survive: %q, %v", got, rerr)
	}
	if entries, rerr := os.ReadDir(moved); rerr != nil || len(entries) != 0 {
		t.Errorf("the staging directory made, at %s, holds %d entries after the refusal, want none (%v)", moved, len(entries), rerr)
	}
}

// TestBackupHintsAtNoHardLinksOnlyForTheNotSupportedErrnos fails the backup's link with each errno
// in turn, through the linkBackup seam. A not-supported errno must carry the hard-link hint. EEXIST
// must be the already-exists refusal with no hint, and so must any other failure. Each case must
// leave no staging directory behind and no output name.
func TestBackupHintsAtNoHardLinksOnlyForTheNotSupportedErrnos(t *testing.T) {
	cases := map[string]struct {
		err    error
		hint   bool
		exists bool // the refusal must be the already-exists one
	}{
		"EPERM":      {syscall.EPERM, true, false},
		"ENOTSUP":    {syscall.ENOTSUP, true, false},
		"EOPNOTSUPP": {syscall.EOPNOTSUPP, true, false},
		"EXDEV":      {syscall.EXDEV, true, false},
		"ENOSYS":     {syscall.ENOSYS, true, false},
		"EEXIST":     {syscall.EEXIST, false, true},
		"EIO":        {syscall.EIO, false, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			db := filepath.Join(dir, "cairn.db")
			createDB(t, db)
			out := filepath.Join(dir, "backup.db")

			prev := linkBackup
			linkBackup = func(oldname, newname string) error {
				return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: c.err}
			}
			t.Cleanup(func() { linkBackup = prev })

			_, err := Backup(context.Background(), db, out)
			if err == nil {
				t.Fatalf("Backup with the link failing with %s: want a refusal", name)
			}
			if got := strings.Contains(err.Error(), "does not support hard links"); got != c.hint {
				t.Errorf("Backup with the link failing with %s: hint = %v, want %v (err: %v)", name, got, c.hint, err)
			}
			if got := strings.Contains(err.Error(), "already exists"); got != c.exists {
				t.Errorf("Backup with the link failing with %s: already-exists refusal = %v, want %v (err: %v)", name, got, c.exists, err)
			}
			if _, serr := os.Lstat(out); !os.IsNotExist(serr) {
				t.Errorf("a refused backup left the output %s behind", out)
			}
			entries, rerr := os.ReadDir(dir)
			if rerr != nil {
				t.Fatal(rerr)
			}
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".cairn-backup-") {
					t.Errorf("a refused backup left the staging directory %s behind", e.Name())
				}
			}
		})
	}
}
