package server

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/djm56/cairn-memory/internal/store"
)

// dbFileMode is the mode of a database file the server creates (DESIGN §5.3). The file
// holds every project's data, so no other user on the host may read it. SQLite gives
// the -wal and -shm files the mode of the database file, so they follow it, and the
// umask can only take bits away from it.
const dbFileMode = 0o600

// checkDBPath refuses a database name that is not a file the server can keep its data
// in. The store appends its options after a '?', so a name with one is refused. A
// memory database, ":memory:" or a "file:" URI, is refused too: its data is gone when
// the process exits, and a server that loses every key and task on restart is a fault
// the operator would not see until it happened (DESIGN §5.3).
func checkDBPath(path string) error {
	switch {
	case path == "":
		return errors.New("--db is required: the path of the SQLite file the server keeps its data in")
	case strings.Contains(path, "?"):
		return errors.New("--db must not contain '?': the driver reads it as the start of its options")
	case path == ":memory:" || strings.HasPrefix(path, "file:"):
		return errors.New("--db must be a file path: a memory database or a file: URI loses its data when the process exits")
	}
	return nil
}

// openDatabase opens the store at path after checkDBPath. When create is true and the
// file is absent, it is made first with dbFileMode, so no other user can read it in the
// gap before the store opens it (O_EXCL, so an existing file is never changed). When
// create is false the file must already exist: a key command that lists or revokes
// names a database a server made, and a mistyped path must not leave an empty one
// behind.
func openDatabase(path string, create bool) (*store.Store, error) {
	if err := checkDBPath(path); err != nil {
		return nil, err
	}
	if create {
		if err := refuseNewOverRestore(path); err != nil {
			return nil, err
		}
		if err := createDatabaseFile(path); err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("no database at %s: the server creates it, and cairn key add creates it too", path)
		}
		return nil, fmt.Errorf("the database %s: %w", path, err)
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open the database: %w", err)
	}
	return st, nil
}

// refuseNewOverRestore refuses to create a database at path when a restore moved one aside and
// the path is now empty: a file named <path>.pre-restore-* is beside it. Creating then would make
// an empty server where the restore was interrupted, and the database that was there would look
// lost. The same files are kept for good after a restore that succeeded, so the refusal can be
// stale. The error therefore says "may", names the newest stamp (the database it moved aside
// last), and lists every stamp found, so an old copy is not taken for the current one (14b-W3).
// Nothing is created and nothing is moved. A path that exists is not refused here.
func refuseNewOverRestore(path string) error {
	if _, err := os.Lstat(path); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("look for the files of an interrupted restore beside %s: %w", path, err)
	}
	prefix := base + ".pre-restore-"
	var found []string // names, sorted by os.ReadDir, so the main file of a stamp sorts first
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			found = append(found, e.Name())
		}
	}
	if len(found) == 0 {
		return nil
	}
	newest, stamps := newestPreRestore(found, prefix)
	return fmt.Errorf("no database is at %s, but files of an earlier restore are beside it, and a restore may have been interrupted. Nothing was created. The newest is %s, moved aside at stamp %s; the stamps found are %s, and the files are %s. If you are not recovering from a restore, and the database was restored or removed on purpose long ago, these files are history, not an interrupted restore. To put the newest back, stop cairn serve, move any file now at %s aside, then run: mv -- %s %s (README, If a restore fails)",
		path, filepath.Join(dir, newest.name), newest.stamp, strings.Join(stamps, ", "), joinNames(dir, found), path, shellQuote(filepath.Join(dir, newest.name)), shellQuote(path))
}

// preRestoreFile is a file beside a database that a restore moved aside: its name and the UTC
// stamp in that name.
type preRestoreFile struct {
	name, stamp string
}

// newestPreRestore returns the pre-restore file with the newest stamp among names, which all begin
// with prefix, and every distinct stamp found, oldest first. A stamp is fixed-width UTC, so string
// order is time order. The newest name is the main file of its stamp when that exists, since
// found is sorted and the main file's name is the shorter.
func newestPreRestore(found []string, prefix string) (preRestoreFile, []string) {
	var best preRestoreFile
	seen := map[string]bool{}
	var stamps []string
	for _, name := range found {
		rest := strings.TrimPrefix(name, prefix)
		stamp := rest
		if len(rest) >= len(preRestoreStamp) {
			stamp = rest[:len(preRestoreStamp)]
		}
		if !seen[stamp] {
			seen[stamp] = true
			stamps = append(stamps, stamp)
		}
		if best.name == "" || stamp > best.stamp {
			best = preRestoreFile{name: name, stamp: stamp}
		}
	}
	slices.Sort(stamps)
	return best, stamps
}

// joinNames returns the names, each joined to dir, in one comma-separated list.
func joinNames(dir string, names []string) string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = filepath.Join(dir, n)
	}
	return strings.Join(out, ", ")
}

// createDatabaseFile makes the file at path with dbFileMode when it does not exist. An
// existing file is left as it is, mode and all.
func createDatabaseFile(path string) error {
	// A dangling symbolic link at path is refused by name. The exclusive create below fails on
	// it with EEXIST, which reads as "already exists", and the store would then create the link's
	// target at SQLite's default mode, which is not dbFileMode (8-S3). A link to a file that exists
	// is not refused here: the store opens that file, and its mode is the one it has.
	if info, err := os.Lstat(path); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		if _, serr := os.Stat(path); errors.Is(serr, fs.ErrNotExist) {
			return fmt.Errorf("the database %s is a symbolic link to a file that does not exist: point the link at a real file, or remove it", path)
		}
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, dbFileMode)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create the database %s: %w", path, err)
	}
	return f.Close()
}
