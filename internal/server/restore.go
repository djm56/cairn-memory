package server

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/djm56/cairn-memory/internal/store"
)

// preRestoreStamp is the UTC time in a pre-restore name: 20261031T101112Z. It has no colon, so
// it is a file name on every system the server runs on.
const preRestoreStamp = "20060102T150405Z"

// RestoreOptions is what cairn restore starts with. DB is the database to replace and Backup
// the copy to restore from. Terminal says stdin is a terminal: the command checks it, and a
// test sets it. Input is where the confirmation is typed. Out gets the prompt and the report,
// and Now is the clock for the pre-restore name; nil means the real one.
type RestoreOptions struct {
	DB       string
	Backup   string
	Terminal bool
	Input    io.Reader
	Out      io.Writer
	Now      func() time.Time
}

// Restore replaces the database at o.DB with the backup at o.Backup. It runs in this order, and
// stops at the first step that refuses, with nothing changed:
//
//  1. the backup is copied beside the database and checked there: it opens, passes PRAGMA
//     integrity_check, and its schema is not newer than this build's migrations;
//  2. the database, if it exists, is not in use: an exclusive lock is taken, and an idle server
//     holding it is enough to refuse (store.AcquireExclusive);
//  3. stdin is a terminal, and the operator types the database path exactly as given;
//  4. the current database and its -wal and -shm files are moved to pre-restore names, never
//     deleted;
//  5. the checked copy is moved into place, and the database is opened, which migrates it to
//     this build's schema.
//
// The messages carry no "cairn restore:" prefix; the command adds it.
func Restore(ctx context.Context, o RestoreOptions) error {
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if err := checkDBPath(o.DB); err != nil {
		return err
	}
	if o.Backup == "" {
		return errors.New("needs a backup file: cairn restore --db <file> <backup>")
	}
	bi, err := os.Stat(o.Backup)
	if err != nil {
		return fmt.Errorf("cannot read the backup %s: %w", o.Backup, err)
	}
	if !bi.Mode().IsRegular() {
		return fmt.Errorf("the backup %s is not a regular file", o.Backup)
	}
	di, derr := os.Stat(o.DB)
	exists := derr == nil
	if exists && os.SameFile(di, bi) {
		return fmt.Errorf("the backup %s is the database itself: restore from a copy made by cairn backup", o.Backup)
	}

	// 1. Stage and verify the copy. It sits beside the database, so the final rename stays on one
	// file system, and it is removed unless it is moved into place.
	staged, version, err := stageBackup(ctx, o.DB, o.Backup)
	if err != nil {
		return err
	}
	defer func() {
		if staged != "" {
			_ = os.Remove(staged)
		}
	}()
	fmt.Fprintf(o.Out, "backup %s: verified (integrity ok, schema version %d)\n", o.Backup, version)

	// 2. The database must not be in use. The lock is held through the moves below, so a server
	// that starts meanwhile is refused too, and it is released once the files have moved.
	var lock *store.ExclusiveLock
	defer func() {
		if lock != nil {
			_ = lock.Release()
		}
	}()
	if exists {
		lock, err = store.AcquireExclusive(ctx, o.DB)
		if errors.Is(err, store.ErrInUse) {
			return fmt.Errorf("the database %s is in use: stop cairn serve, or whatever holds it, then try again; nothing was changed", o.DB)
		}
		if err != nil {
			return err
		}
	}

	// 3. A person confirms, at a terminal, by typing the path.
	if !o.Terminal {
		return errors.New("stdin is not a terminal: the restore asks you to type the database path, so run it from an interactive shell; nothing was changed")
	}
	stamp := o.Now().UTC().Format(preRestoreStamp)
	pre := o.DB + ".pre-restore-" + stamp
	fmt.Fprintf(o.Out, "This replaces the database at %s with the backup %s.\n", o.DB, o.Backup)
	fmt.Fprintf(o.Out, "The database there now is moved, with its -wal and -shm files, to %s. Nothing is deleted.\n", pre)
	fmt.Fprintf(o.Out, "To confirm, type the database path exactly as given: ")
	typed, err := readLine(o.Input)
	if err != nil {
		return err
	}
	if typed != o.DB {
		return fmt.Errorf("the path typed does not match %s; nothing was changed", o.DB)
	}

	// 4. Move the current database aside, with its -wal and -shm. Each target name must be free,
	// checked before the first move.
	if exists {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if _, serr := os.Lstat(pre + suffix); serr == nil {
				return fmt.Errorf("the name %s is taken; nothing was changed", pre+suffix)
			}
		}
		if err := moveAside(o.DB, pre); err != nil {
			return err
		}
		fmt.Fprintf(o.Out, "moved %s to %s\n", o.DB, pre)
		for _, suffix := range []string{"-wal", "-shm"} {
			if _, serr := os.Lstat(o.DB + suffix); serr == nil {
				fmt.Fprintf(o.Out, "moved %s to %s\n", o.DB+suffix, pre+suffix)
			}
		}
		if lock != nil {
			// The probe's connection still holds the moved files: releasing it now lets SQLite
			// checkpoint what it holds into the file that was moved, and no file is named again.
			err := lock.Release()
			lock = nil
			if err != nil {
				return fmt.Errorf("release the lock on %s: %w; the database was moved to %s and the restore stopped there", o.DB, err, pre)
			}
		}
	} else {
		fmt.Fprintf(o.Out, "no database was at %s: nothing was moved\n", o.DB)
	}

	// 5. Put the checked copy in place, and open it. Opening migrates it to this build's schema.
	if err := os.Rename(staged, o.DB); err != nil {
		return fmt.Errorf("put the backup in place at %s: %w; the database before the restore is at %s", o.DB, err, pre)
	}
	staged = ""
	st, err := store.Open(o.DB)
	if err != nil {
		return fmt.Errorf("the restored database at %s did not open: %w; the database before the restore is at %s", o.DB, err, pre)
	}
	defer st.Close()
	v, err := st.Version(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(o.Out, "restored %s from %s; the schema is at version %d\n", o.DB, o.Backup, v)
	return nil
}

// stageBackup copies the backup at src to a new file in the directory of db, with dbFileMode
// (os.CreateTemp makes it 0600), then checks the copy as Restore needs: it opens as a Cairn
// database, passes the integrity check, and its schema is not newer than this build. It returns
// the copy's name and the schema version. On a refusal the copy is removed.
func stageBackup(ctx context.Context, db, src string) (string, int, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", 0, fmt.Errorf("cannot read the backup %s: %w", src, err)
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(db), filepath.Base(db)+".restore-*.tmp")
	if err != nil {
		return "", 0, fmt.Errorf("cannot stage the restore beside %s: %w", db, err)
	}
	name := tmp.Name()
	fail := func(err error) (string, int, error) {
		_ = os.Remove(name)
		return "", 0, err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return fail(fmt.Errorf("copy the backup %s: %w", src, err))
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fail(fmt.Errorf("copy the backup %s: %w", src, err))
	}
	if err := tmp.Close(); err != nil {
		return fail(fmt.Errorf("copy the backup %s: %w", src, err))
	}
	st, err := store.OpenExisting(name)
	if err != nil {
		return fail(fmt.Errorf("the backup %s is not usable: %w", src, err))
	}
	defer st.Close()
	if err := st.IntegrityCheck(ctx); err != nil {
		return fail(fmt.Errorf("the backup %s is corrupt: %w", src, err))
	}
	v, err := st.Version(ctx)
	if err != nil {
		return fail(err)
	}
	return name, v, nil
}

// moveAside renames the database at db, and its -wal and -shm if they exist, to the same names
// under pre. The database itself is moved first. A rename that fails undoes the ones already done.
func moveAside(db, pre string) error {
	var done []string
	for _, suffix := range []string{"", "-wal", "-shm"} {
		src := db + suffix
		if _, err := os.Lstat(src); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := os.Rename(src, pre+suffix); err != nil {
			for i := len(done) - 1; i >= 0; i-- {
				_ = os.Rename(pre+done[i], db+done[i])
			}
			return fmt.Errorf("move %s aside: %w; nothing was deleted, and the database is as it was", src, err)
		}
		done = append(done, suffix)
	}
	return nil
}

// readLine reads one line from in, without its line ending. A missing reader or no input at all
// is no confirmation, so it is an error.
func readLine(in io.Reader) (string, error) {
	if in == nil {
		return "", errors.New("no confirmation was given; nothing was changed")
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read the confirmation: %w; nothing was changed", err)
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return "", errors.New("no confirmation was given; nothing was changed")
	}
	return line, nil
}
