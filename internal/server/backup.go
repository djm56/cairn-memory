package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/djm56/cairn-memory/internal/store"
)

// checkBackupOut refuses an output name that cannot hold a backup. A backup is a file, so a
// memory database, a file: URI and a name with '?' are refused, as the server refuses them for
// its own database (DESIGN §5.3). An empty name is no name.
func checkBackupOut(out string) error {
	switch {
	case out == "":
		return errors.New("needs an output file: cairn backup --db <file> <out>")
	case strings.Contains(out, "?"):
		return errors.New("the backup file must not contain '?': the driver reads it as the start of its options")
	case out == ":memory:" || strings.HasPrefix(out, "file:"):
		return errors.New("the backup must be a file path: a memory database or a file: URI keeps no copy")
	}
	return nil
}

// Backup writes a consistent copy of the database at db to a new file at out, and returns the
// copy's size in bytes. The server may be running. An existing name at out is refused, a symbolic
// link included, and is never written over. The copy is owner-only (dbFileMode). The database is
// opened without migrating it, so a backup never changes the database it copies. A database with
// no Cairn schema, or with one newer than this build, is refused.
//
// The copy is never written at out. It is made in a directory of its own, created beside out by
// os.MkdirTemp: that directory is 0700 and its name is unpredictable. The finished copy is then
// given the name out with os.Link, which fails if any entry holds the name, a symbolic link
// included. So a copy cannot be written through a link at out (#129), and nothing is overwritten.
// The staging name is removed once the copy is linked, and the directory of out is flushed.
//
// Consistency under concurrent writes is the store's (store.Store.Backup): the copy holds every
// commit that finished before the call and none that did not, in one piece.
func Backup(ctx context.Context, db, out string) (int64, error) {
	if err := checkBackupOut(out); err != nil {
		return 0, err
	}
	if err := checkDBPath(db); err != nil {
		return 0, err
	}
	if _, err := os.Stat(db); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, fmt.Errorf("no database at %s: a backup copies a database that exists", db)
		}
		return 0, fmt.Errorf("the database %s: %w", db, err)
	}
	st, err := store.OpenExisting(db)
	if err != nil {
		return 0, err
	}
	defer st.Close()

	// An early look at the name, so a refusal costs no copy. The link below is the check that counts.
	if _, err := os.Lstat(out); err == nil {
		return 0, errExistsBackup(out)
	}
	stage, err := os.MkdirTemp(filepath.Dir(out), ".cairn-backup-*")
	if err != nil {
		return 0, fmt.Errorf("create the staging directory for the backup beside %s: %w", out, err)
	}
	tmp := filepath.Join(stage, "copy.db")
	// VACUUM INTO writes into a file that does not exist yet. The file it makes has the default
	// mode, so it is set to dbFileMode before it is linked. The directory is 0700, so no other
	// user can reach the file in the meantime.
	if err := copyBackup(ctx, st, tmp); err != nil {
		return 0, errors.Join(err, removeStage(stage))
	}
	if err := os.Chmod(tmp, dbFileMode); err != nil {
		return 0, errors.Join(fmt.Errorf("set the mode of the backup copy for %s: %w", out, err), removeStage(stage))
	}
	// The copy is on disk before its name is made (14-S7). A copy that cannot be flushed is not linked.
	if err := syncPath(tmp); err != nil {
		return 0, errors.Join(fmt.Errorf("flush the backup file %s: %w", out, err), removeStage(stage))
	}
	info, err := os.Stat(tmp)
	if err != nil {
		return 0, errors.Join(fmt.Errorf("read the backup file %s: %w", out, err), removeStage(stage))
	}
	// The name. Link fails with EEXIST on any entry it finds, a symbolic link included, so an
	// existing file or link at out is refused, and nothing is written through it.
	if err := os.Link(tmp, out); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return 0, errors.Join(errExistsBackup(out), removeStage(stage))
		}
		return 0, errors.Join(fmt.Errorf("create the backup file %s: %w", out, err), removeStage(stage))
	}
	// out holds the copy now. The staging name goes. If it cannot be removed, the backup is not
	// reported made, so out, which this call created, is removed as well.
	if err := removeStage(stage); err != nil {
		_ = os.Remove(out)
		return 0, fmt.Errorf("remove the staging copy of the backup %s: %w", out, err)
	}
	// The new name is durable before the backup is reported made (14-S7).
	if err := syncPath(filepath.Dir(out)); err != nil {
		_ = os.Remove(out)
		return 0, fmt.Errorf("flush the directory of the backup file %s: %w", out, err)
	}
	return info.Size(), nil
}

// errExistsBackup is the refusal for an output name that is taken, whether by a file or a link.
func errExistsBackup(out string) error {
	return fmt.Errorf("the backup file %s already exists: a backup is made to a new file, and an existing one is never written over", out)
}

// removeStage removes what is in the staging directory stage, then the directory. Each entry is
// removed by name and is not followed, and a directory that still holds something is an error, so
// nothing that is not the copy is removed. A file that was linked to out is not touched by this:
// removing the staging name leaves the link in place.
func removeStage(stage string) error {
	entries, err := os.ReadDir(stage)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range entries {
		if err := os.Remove(filepath.Join(stage, e.Name())); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return os.Remove(stage)
}

// copyBackup writes the copy of st to path, a name that does not exist yet in a directory this
// call made. It is st.Backup. A test replaces it to make the copy fail part way, or to place a
// link at the output name while the copy is being made, so both can be checked.
var copyBackup = func(ctx context.Context, st *store.Store, path string) error {
	return st.Backup(ctx, path)
}

// syncFile flushes an open file or directory. It is f.Sync; a test replaces it to make the flush
// fail, and puts it back when it ends.
var syncFile = func(f *os.File) error { return f.Sync() }

// syncPath flushes the file or directory at p to stable storage. A directory is opened read-only
// and synced, which makes the names in it durable.
//
// Some file systems refuse a sync of a directory with EINVAL or ENOTSUP (certain network and FUSE
// mounts do). On those, a directory sync cannot be made, so this call treats the directory as
// synced and carries on rather than failing every backup and restore on that mount (14b-S5). The
// tolerance is for directories only: a file that cannot be flushed is still an error, and so is
// any other error from a directory sync.
func syncPath(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	serr := syncFile(f)
	fi, statErr := f.Stat()
	isDir := statErr == nil && fi.IsDir()
	if isDir && (errors.Is(serr, syscall.EINVAL) || errors.Is(serr, syscall.ENOTSUP)) {
		serr = nil
	}
	return errors.Join(serr, f.Close())
}
