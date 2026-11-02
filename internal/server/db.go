package server

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
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
