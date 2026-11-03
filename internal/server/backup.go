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

// copyName is the name of the copy inside the staging directory.
const copyName = "copy.db"

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
// os.MkdirTemp: that directory is 0700 and its name is unpredictable. The directory is opened once
// as an os.Root, and everything removed from it is removed through that root, so the removal stays
// bound to the directory made and never follows a name that has been swapped (#129, W1). The copy
// is then given the name out with os.Link, which fails if any entry holds the name, a symbolic link
// included. So a copy cannot be written through a link at out (#129), and nothing is overwritten.
// The staging name is removed once the copy is linked, and the directory of out is flushed.
//
// The swap guard. Whoever may write to the output directory may rename the staging directory and
// put a link at its name. checkStage refuses that: it compares the name, and the directory the root
// holds, with the directory made. It runs before the copy, after the copy, and before the link.
// Two windows are narrowed, not closed. VACUUM INTO takes a path, so the copy is written by name:
// a swap during the copy sends the bytes to the directory swapped in. copyOwnerOnly sets the
// umask to 077 for that copy, so the file is owner-only wherever it lands, and the copy is then
// refused, since the root holds no copy. The link is also a call by name. A swap in the moment
// between the last check and the link would link a file from the directory swapped in. The
// result is checked against the copy made, and the name is removed if it is not that copy. The
// output directory must not be writable by other users (docs/DEPLOY.md, Backup).
//
// Consistency under concurrent writes is the store's (store.Store.Backup): the copy holds every
// commit that finished before the call and none that did not, in one piece.
//
// Backup must be called one at a time, and never from serve. The copy sets the process umask
// (copyOwnerOnly), which is process-wide, so two calls at once would interleave their set and
// restore. The set and restore pair is not re-entrant. The cairn backup command is the only caller.
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
	// made is the staging directory as it was created. Every later check compares against it.
	made, err := os.Lstat(stage)
	if err != nil {
		return 0, errors.Join(fmt.Errorf("read the staging directory %s: %w", stage, err), os.Remove(stage))
	}
	sr, err := os.OpenRoot(stage)
	if err != nil {
		return 0, errors.Join(fmt.Errorf("open the staging directory %s: %w", stage, err), os.Remove(stage))
	}
	defer sr.Close()
	if err := checkStage(sr, stage, made); err != nil {
		return 0, err
	}

	// VACUUM INTO writes into a file that does not exist yet. The copy is made under a umask of
	// 077, so the file has no permission for anyone but this user, whatever the default mode is.
	// It is then set to dbFileMode, through the root, before it is linked.
	tmp := filepath.Join(stage, copyName)
	if err := copyOwnerOnly(ctx, st, tmp); err != nil {
		return 0, errors.Join(err, removeStage(sr, stage, made))
	}
	afterCopy(stage)
	if err := checkStage(sr, stage, made); err != nil {
		return 0, errors.Join(err, removeEntries(sr))
	}
	if err := sr.Chmod(copyName, dbFileMode); err != nil {
		return 0, errors.Join(fmt.Errorf("set the mode of the backup copy for %s: %w", out, err), removeStage(sr, stage, made))
	}
	// The copy is on disk before its name is made (14-S7). A copy that cannot be flushed is not linked.
	if err := syncPath(tmp); err != nil {
		return 0, errors.Join(fmt.Errorf("flush the backup file %s: %w", out, err), removeStage(sr, stage, made))
	}
	copyInfo, err := sr.Stat(copyName)
	if err != nil {
		return 0, errors.Join(fmt.Errorf("read the backup file %s: %w", out, err), removeStage(sr, stage, made))
	}
	// The name. Link fails with EEXIST on any entry it finds, a symbolic link included, so an
	// existing file or link at out is refused, and nothing is written through it.
	if err := checkStage(sr, stage, made); err != nil {
		return 0, errors.Join(err, removeEntries(sr))
	}
	beforeLink(tmp)
	if err := os.Link(tmp, out); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return 0, errors.Join(errExistsBackup(out), removeStage(sr, stage, made))
		}
		lerr := fmt.Errorf("create the backup file %s: %w", out, err)
		if noHardLinks(err) {
			lerr = fmt.Errorf("%w (this filesystem does not support hard links, which the backup needs: the output directory must be on one that does)", lerr)
		}
		return 0, errors.Join(lerr, removeStage(sr, stage, made))
	}
	// The name holds whatever tmp named at the link. It must be the copy made above, or the link
	// followed a swapped staging directory (see Backup). Then the name is removed, and nothing is
	// reported made.
	if now, err := os.Lstat(out); err != nil || !os.SameFile(now, copyInfo) {
		_ = os.Remove(out)
		return 0, errors.Join(fmt.Errorf("the backup file %s is not the copy made: the staging directory %s was swapped while the copy was linked, and the name was removed", out, stage), removeEntries(sr))
	}
	// out holds the copy now. The staging name goes. If it cannot be removed, the backup is not
	// reported made, so out, which this call created, is removed as well.
	if err := removeStage(sr, stage, made); err != nil {
		_ = os.Remove(out)
		return 0, fmt.Errorf("remove the staging copy of the backup %s: the staging directory %s may be left with a partial copy: %w", out, stage, err)
	}
	// The new name is durable before the backup is reported made (14-S7).
	if err := syncPath(filepath.Dir(out)); err != nil {
		_ = os.Remove(out)
		return 0, fmt.Errorf("flush the directory of the backup file %s: %w", out, err)
	}
	return copyInfo.Size(), nil
}

// errExistsBackup is the refusal for an output name that is taken, whether by a file or a link.
func errExistsBackup(out string) error {
	return fmt.Errorf("the backup file %s already exists: a backup is made to a new file, and an existing one is never written over", out)
}

// checkStage refuses unless the staging directory is still the one made. It compares two things:
// the name stage, which a swap changes, and the directory sr holds, which was opened at creation
// and does not change. Both must be made's directory. A refusal names stage and removes nothing.
func checkStage(sr *os.Root, stage string, made os.FileInfo) error {
	now, err := os.Lstat(stage)
	if err != nil || !os.SameFile(now, made) {
		return fmt.Errorf("the staging directory %s was replaced while the backup was made, so the backup is refused and nothing at that name was removed", stage)
	}
	held, err := sr.Stat(".")
	if err != nil || !os.SameFile(held, made) {
		return fmt.Errorf("the staging directory %s does not hold the directory the backup made, so the backup is refused", stage)
	}
	return nil
}

// removeEntries removes every entry of the staging directory, through sr. The root is bound to
// the directory made, so an entry is removed from there even when the name has been swapped, and
// a link is removed as a link, never followed. A directory that still holds something is an error.
func removeEntries(sr *os.Root) error {
	dir, err := sr.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	cerr := dir.Close()
	if err != nil {
		return errors.Join(err, cerr)
	}
	var errs []error
	for _, e := range entries {
		if err := sr.Remove(e.Name()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(append(errs, cerr)...)
}

// removeStage removes the entries of the staging directory, then the directory itself. The
// directory is removed by name, so checkStage must pass first: a name that is no longer the
// directory made is not removed. Nothing that is not the copy is removed, and a link at out is
// not touched: removing the staging name leaves the link in place.
func removeStage(sr *os.Root, stage string, made os.FileInfo) error {
	if err := removeEntries(sr); err != nil {
		return err
	}
	if err := checkStage(sr, stage, made); err != nil {
		return err
	}
	return os.Remove(stage)
}

// copyOwnerOnly makes the copy at path with copyBackup, under a umask of 077, so the file
// VACUUM INTO creates is owner-only whatever the default mode is. The umask is process-wide, so
// it is set for the copy alone and put back when the copy returns. On a platform with no umask
// (setOwnerOnlyUmask) the copy is not owner-only until the chmod that follows it.
func copyOwnerOnly(ctx context.Context, st *store.Store, path string) error {
	restore := setOwnerOnlyUmask()
	defer restore()
	return copyBackup(ctx, st, path)
}

// noHardLinks reports whether a failed link means the filesystem cannot make one: EPERM (FAT and
// exFAT on macOS), ENOTSUP and EOPNOTSUPP, EXDEV, and ENOSYS. It is false for EEXIST and every
// other failure. The list is the one cmd/cairn's noHardLinks uses (init.go). The two are kept in
// step by hand, because package main cannot be imported.
func noHardLinks(err error) bool {
	for _, errno := range []syscall.Errno{syscall.EPERM, syscall.ENOTSUP, syscall.EOPNOTSUPP, syscall.EXDEV, syscall.ENOSYS} {
		if errors.Is(err, errno) {
			return true
		}
	}
	return false
}

// afterCopy runs once the copy is in place, before the check that follows it. It does nothing. A
// test replaces it to move the staging name while the root-held copy is still there, so the check
// can be shown to refuse the swap, and puts it back when it ends.
var afterCopy = func(stage string) {}

// beforeLink runs after the check before the link, and before the link itself, with the name of
// the copy. It does nothing. A test replaces it to swap the copy between the last check and the
// link, so the check on the linked file can be shown to refuse the swap.
var beforeLink = func(tmp string) {}

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
