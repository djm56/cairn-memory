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

// dbSet is the suffixes of a database's files: the database itself, its -wal and its -shm.
var dbSet = []string{"", "-wal", "-shm"}

// Seams. Each is the real operation in production; a test replaces one to make that step fail,
// and puts it back when it ends. Tests that replace them do not run in parallel.
var (
	// placeCopy puts the checked copy at the database path (step 5).
	placeCopy = os.Rename
	// openRestored opens the restored copy, which migrates it to this build's schema (step 5).
	openRestored = store.Open
	// releaseLock gives up the probe's lock once the files have moved (step 4).
	releaseLock = func(l *store.ExclusiveLock) error { return l.Release() }
)

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

// move is one file Restore moved: from its name, to its pre-restore name.
type move struct {
	from, to string
}

// Restore replaces the database at o.DB with the backup at o.Backup. It runs in this order, and
// stops at the first step that refuses, with nothing changed unless the error says otherwise:
//
//  1. stdin must be a terminal. Nothing is read, locked or copied before this is known.
//  2. the backup is copied beside the database and checked there: it opens, passes PRAGMA
//     integrity_check, and its schema is not newer than this build's migrations;
//  3. the database, if it exists, is not in use: an exclusive lock is taken, and an idle server
//     holding it is enough to refuse (store.AcquireExclusive). A file that is not a readable
//     SQLite database cannot be in use, so it takes no lock and goes to step 5 as bytes;
//  4. the operator types the database path exactly as given;
//  5. for a database that is readable, its -wal is folded into it and it leaves WAL mode, so
//     the file alone holds the state (store.ExclusiveLock.Consolidate). A file that is not
//     readable is not checkpointed. Then the files of the set that exist (the database, -wal and
//     -shm) are moved to pre-restore names, each reported, and never deleted. An absent database
//     with a stray -wal or -shm beside it has those moved the same way;
//  6. the checked copy is renamed to the database path, and the database is opened, which
//     migrates it to this build's schema.
//
// Every error after the first move names the pre-restore path and the step that puts the
// database back (see recoveryStep). The messages carry no "cairn restore:" prefix; the command
// adds it.
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
	// 1. Checked first, so a restore that cannot be confirmed does nothing else (14-S1).
	if !o.Terminal {
		return errors.New("stdin is not a terminal: the restore asks you to type the database path, so run it from an interactive shell; nothing was changed")
	}
	bi, err := os.Stat(o.Backup)
	if err != nil {
		return fmt.Errorf("cannot read the backup %s: %w", o.Backup, err)
	}
	if !bi.Mode().IsRegular() {
		return fmt.Errorf("the backup %s is not a regular file", o.Backup)
	}
	// A link at a name of the set is refused. A restore moves the file a name holds, and a link
	// would be moved in place of the file it points to, which would stay where it was (14-S2).
	for _, suffix := range dbSet {
		if li, lerr := os.Lstat(o.DB + suffix); lerr == nil && li.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symbolic link: point --db at the database file itself; nothing was changed", o.DB+suffix)
		}
	}
	di, derr := os.Stat(o.DB)
	exists := derr == nil
	if derr != nil && !errors.Is(derr, fs.ErrNotExist) {
		return fmt.Errorf("the database %s: %w; nothing was changed", o.DB, derr)
	}
	if exists && os.SameFile(di, bi) {
		return fmt.Errorf("the backup %s is the database itself: restore from a copy made by cairn backup", o.Backup)
	}

	// 2. Stage and verify the copy. It sits beside the database, so the final rename stays on one
	// file system, and it is removed unless it is moved into place.
	staged, version, err := stageBackup(ctx, o.DB, o.Backup)
	if err != nil {
		return err
	}
	defer func() {
		if staged != "" {
			_ = removeStaged(staged)
		}
	}()
	fmt.Fprintf(o.Out, "backup %s: verified (integrity ok, schema version %d)\n", o.Backup, version)

	// 3. The database must not be in use. The lock is held through the moves below, so a server
	// that starts meanwhile is refused too, and it is released once the files have moved. A file
	// that is not a readable database takes no lock: nothing can be using it as one.
	var lock *store.ExclusiveLock
	defer func() {
		if lock != nil {
			_ = lock.Release()
		}
	}()
	// The readability check is read-only: a read-write open of a file that cannot be read makes
	// SQLite delete the -wal beside it on close, so an unreadable file is never opened that way.
	raw := false
	if exists {
		err = store.CheckReadable(ctx, o.DB)
		switch {
		case errors.Is(err, store.ErrNotDatabase):
			raw = true
		case err != nil:
			return err
		default:
			lock, err = store.AcquireExclusive(ctx, o.DB)
			switch {
			case errors.Is(err, store.ErrInUse):
				return fmt.Errorf("the database %s is in use: stop cairn serve, or whatever holds it, then try again; nothing was changed", o.DB)
			case err != nil:
				return err
			}
		}
	}
	stamp := o.Now().UTC().Format(preRestoreStamp)
	pre := o.DB + ".pre-restore-" + stamp
	strays := []string(nil)
	if !exists {
		strays = presentSuffixes(o.DB, []string{"-wal", "-shm"})
	}

	// 4. A person confirms, at a terminal, by typing the path.
	if exists {
		fmt.Fprintf(o.Out, "This replaces the database at %s with the backup %s.\n", o.DB, o.Backup)
		fmt.Fprintf(o.Out, "The database there now is moved to %s, after its -wal is folded into it, with any -shm beside it. Nothing is deleted.\n", pre)
		if raw {
			fmt.Fprintf(o.Out, "%s is not a readable SQLite database: its files are moved as they are, with no checkpoint.\n", o.DB)
		}
	} else {
		fmt.Fprintf(o.Out, "No database is at %s now. This creates it from the backup %s.\n", o.DB, o.Backup)
		if len(strays) > 0 {
			fmt.Fprintf(o.Out, "A -wal or -shm is at that path with no database beside it: it is moved to %s first. Nothing is deleted.\n", pre)
		}
	}
	fmt.Fprintf(o.Out, "To confirm, type the database path exactly as given: ")
	typed, err := readLine(o.Input)
	if err != nil {
		return err
	}
	if typed != o.DB {
		return fmt.Errorf("the path typed does not match %s; nothing was changed", o.DB)
	}

	// 5. Fold the database's -wal into it, then move the files. Each target name must be free,
	// checked before the first change. The checkpoint is made under the lock, before any move.
	var moves []move
	all := presentSuffixes(o.DB, dbSet)
	if err := checkFree(pre, all); err != nil {
		return err
	}
	if lock != nil {
		if err := lock.Consolidate(ctx); err != nil {
			return fmt.Errorf("fold the -wal of %s into it before any move: %w; nothing was moved", o.DB, err)
		}
	}
	if len(all) > 0 {
		done, err := moveAside(o.DB, pre, all)
		moves = append(moves, done...)
		printMoves(o.Out, done)
		if err != nil {
			if len(moves) > 0 {
				return fmt.Errorf("%w; %s", err, recoveryStep(o.DB, pre, moves))
			}
			return err
		}
		// A file of the set that is still there after the moves is moved too. The lock is held,
		// so none should be. It is reported all the same.
		if left := presentSuffixes(o.DB, dbSet); len(left) > 0 {
			more, err := moveAside(o.DB, pre, left)
			moves = append(moves, more...)
			printMoves(o.Out, more)
			if err != nil {
				return fmt.Errorf("%w; %s", err, recoveryStep(o.DB, pre, moves))
			}
		}
		if err := syncPath(filepath.Dir(o.DB)); err != nil {
			return fmt.Errorf("flush the moves in %s: %w; %s", filepath.Dir(o.DB), err, recoveryStep(o.DB, pre, moves))
		}
		if lock != nil {
			lockErr := releaseLock(lock)
			lock = nil
			if lockErr != nil {
				return fmt.Errorf("release the lock on %s: %w; %s", o.DB, lockErr, recoveryStep(o.DB, pre, moves))
			}
		}
		if raw {
			fmt.Fprintf(o.Out, "%s was not a readable SQLite database: its files were moved as they are, and it was not checkpointed\n", o.DB)
		}
	} else {
		fmt.Fprintf(o.Out, "no database was at %s: nothing was moved\n", o.DB)
	}

	// tail is the text an error gives once a move has been made, and nothing otherwise.
	tail := func() string {
		if len(moves) == 0 {
			return "nothing was moved"
		}
		return recoveryStep(o.DB, pre, moves)
	}

	// 6. Put the checked copy in place, and open it. Opening migrates it to this build's schema.
	if err := placeCopy(staged, o.DB); err != nil {
		return fmt.Errorf("put the backup in place at %s: %w; %s", o.DB, err, tail())
	}
	staged = ""
	if err := syncPath(filepath.Dir(o.DB)); err != nil {
		return fmt.Errorf("flush the restored copy in %s: %w; %s", filepath.Dir(o.DB), err, tail())
	}
	st, err := openRestored(o.DB)
	if err != nil {
		return fmt.Errorf("the restored database at %s did not open: %w; %s", o.DB, err, tail())
	}
	defer st.Close()
	v, err := st.Version(ctx)
	if err != nil {
		return fmt.Errorf("read the schema version of the restored database at %s: %w; %s", o.DB, err, tail())
	}
	fmt.Fprintf(o.Out, "restored %s from %s; the schema is at version %d\n", o.DB, o.Backup, v)
	return nil
}

// presentSuffixes returns the suffixes of suffixes whose file exists beside db, in order.
func presentSuffixes(db string, suffixes []string) []string {
	var out []string
	for _, s := range suffixes {
		if _, err := os.Lstat(db + s); err == nil {
			out = append(out, s)
		}
	}
	return out
}

// checkFree refuses when a pre-restore name for one of the suffixes given, each of which has a
// file beside the database, is already taken. Nothing is renamed over a file a restore did not
// make.
func checkFree(pre string, suffixes []string) error {
	for _, s := range suffixes {
		if _, err := os.Lstat(pre + s); err == nil {
			return fmt.Errorf("the name %s is taken; nothing was changed", pre+s)
		}
	}
	return nil
}

// printMoves reports each move, so the report names every file that moved.
func printMoves(w io.Writer, moves []move) {
	for _, m := range moves {
		fmt.Fprintf(w, "moved %s to %s\n", m.from, m.to)
	}
}

// recoveryStep is the text an error gives after a move: where the files that moved are kept, and
// the steps that put them back. The moves are the ones still in effect. When the database itself
// moved, its pre-restore name is named as the database before the restore. When only a stray -wal
// or -shm moved (an absent database), there was no database to keep, and the text says so.
func recoveryStep(db, pre string, moves []move) string {
	cmds := make([]string, 0, len(moves))
	kept := make([]string, 0, len(moves))
	mainMoved := false
	for _, m := range moves {
		cmds = append(cmds, "mv "+m.to+" "+m.from)
		kept = append(kept, m.to)
		if m.from == db {
			mainMoved = true
		}
	}
	what := "the database before the restore is kept at " + pre
	if !mainMoved {
		what = "no database was there before the restore; the files moved aside are kept at " + strings.Join(kept, " and ")
	}
	return fmt.Sprintf("%s. To put them back: stop cairn serve, move any file now at %s aside, then run: %s (see README, If a restore fails)",
		what, db, strings.Join(cmds, " && "))
}

// stageBackup copies the backup at src to a new file in the directory of db, with dbFileMode
// (os.CreateTemp makes it 0600), then checks the copy as Restore needs: it opens as a Cairn
// database, passes the integrity check, and its schema is not newer than this build. It returns
// the copy's name and the schema version. On a refusal the copy and any -wal or -shm beside it
// are removed, after the copy is closed.
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
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return "", 0, errors.Join(fmt.Errorf("copy the backup %s: %w", src, err), removeStaged(name))
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", 0, errors.Join(fmt.Errorf("copy the backup %s: %w", src, err), removeStaged(name))
	}
	if err := tmp.Close(); err != nil {
		return "", 0, errors.Join(fmt.Errorf("copy the backup %s: %w", src, err), removeStaged(name))
	}
	v, err := verifyStaged(ctx, name, src)
	if err != nil {
		return "", 0, errors.Join(err, removeStaged(name))
	}
	return name, v, nil
}

// verifyStaged opens the copy at name as a Cairn database, checks it, and returns its schema
// version. The copy is closed before it returns, so the caller may remove it at once; a close
// that fails is an error.
func verifyStaged(ctx context.Context, name, src string) (v int, err error) {
	st, err := store.OpenExisting(name)
	if err != nil {
		return 0, fmt.Errorf("the backup %s is not usable: %w", src, err)
	}
	defer func() {
		if cerr := st.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close the checked copy of %s: %w", src, cerr)
		}
	}()
	if err := st.IntegrityCheck(ctx); err != nil {
		return 0, fmt.Errorf("the backup %s is corrupt: %w", src, err)
	}
	return st.Version(ctx)
}

// removeStaged removes the staged copy at name and any -wal or -shm beside it. It is called only
// after the copy is closed. A file that is not there is not an error.
func removeStaged(name string) error {
	var errs []error
	for _, suffix := range dbSet {
		if err := os.Remove(name + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// moveAside renames the files of db's set named by suffixes, in the order given, to the same
// suffix under pre. A suffix with no file beside db is skipped. It returns the moves that are
// still in effect. A name already taken at pre is never renamed over. A move that fails undoes
// the moves made before it, in reverse; if an undo fails too, the moves that could not be undone
// are returned with the error, so the caller can say where those files are. Nothing is deleted.
func moveAside(db, pre string, suffixes []string) ([]move, error) {
	var done []move
	for _, suffix := range suffixes {
		src, dst := db+suffix, pre+suffix
		if _, err := os.Lstat(src); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		var err error
		if _, lerr := os.Lstat(dst); lerr == nil {
			err = fmt.Errorf("the name %s is taken", dst)
		} else if err = os.Rename(src, dst); err != nil {
			err = fmt.Errorf("move %s aside: %w", src, err)
		}
		if err != nil {
			stuck, uerr := undoMoves(done)
			if uerr == nil {
				return nil, fmt.Errorf("%w; the moves made before it are undone, and nothing was deleted", err)
			}
			return stuck, errors.Join(err, uerr)
		}
		done = append(done, move{from: src, to: dst})
	}
	return done, nil
}

// undoMoves renames each of moves back, in reverse. It returns the moves it could not undo, and
// the errors of those, joined.
func undoMoves(moves []move) ([]move, error) {
	var stuck []move
	var errs []error
	for i := len(moves) - 1; i >= 0; i-- {
		if err := os.Rename(moves[i].to, moves[i].from); err != nil {
			stuck = append(stuck, moves[i])
			errs = append(errs, fmt.Errorf("undo the move of %s: %w", moves[i].from, err))
		}
	}
	return stuck, errors.Join(errs...)
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
