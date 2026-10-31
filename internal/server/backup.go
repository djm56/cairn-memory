package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

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
// copy's size in bytes. The server may be running. out is created with dbFileMode and O_EXCL:
// an existing file is refused, never written over, and the copy keeps the mode it was made
// with. The database is opened without migrating it, so a backup never changes the database it
// copies. A database with no Cairn schema, or with one newer than this build, is refused.
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

	f, err := os.OpenFile(out, os.O_RDWR|os.O_CREATE|os.O_EXCL, dbFileMode)
	if errors.Is(err, fs.ErrExist) {
		return 0, fmt.Errorf("the backup file %s already exists: a backup is made to a new file, and an existing one is never written over", out)
	}
	if err != nil {
		return 0, fmt.Errorf("create the backup file %s: %w", out, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(out)
		return 0, fmt.Errorf("create the backup file %s: %w", out, err)
	}
	// VACUUM INTO writes into the empty file just made, and keeps its mode. A file it
	// could not finish is removed, so no partial copy is left under the name.
	if err := st.Backup(ctx, out); err != nil {
		_ = os.Remove(out)
		return 0, err
	}
	info, err := os.Stat(out)
	if err != nil {
		return 0, fmt.Errorf("read the backup file %s: %w", out, err)
	}
	return info.Size(), nil
}
