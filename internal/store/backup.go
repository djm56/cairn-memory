package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ErrInUse is returned by AcquireExclusive when another connection has the database open.
// The caller names the database and the fix; the store only reports the fact.
var ErrInUse = errors.New("the database is in use")

// ErrNotDatabase is returned by AcquireExclusive when the file cannot be read as an SQLite
// database at all: its header is not SQLite's, or its schema is damaged past reading. Nothing
// can have it open as a database, so the caller may treat it as a file, not as a database.
var ErrNotDatabase = errors.New("the file is not a readable SQLite database")

// exclusiveBusyTimeout is how long AcquireExclusive waits for a connection that holds the
// database. It is short: a busy database is refused at once, and the caller says so.
const exclusiveBusyTimeout = "250"

// sqliteNotADB and sqliteCorrupt are the primary result codes SQLITE_NOTADB (a file that is not
// a database) and SQLITE_CORRUPT (a database image that is malformed).
const (
	sqliteNotADB   = 26
	sqliteCorrupt  = 11
	readDSNOptions = "_busy_timeout=5000&_foreign_keys=1"
)

// OpenExisting opens the database at path without applying migrations. The file must
// exist and hold a Cairn schema: a file with no schema version is refused as not a Cairn
// database, and one whose version is newer than this build's migrations is refused as
// the schema of a newer build. The backup and restore paths use it, so a backup is never
// changed to a newer schema on the way out, and a backup is verified without being
// migrated.
//
// The connection does not set a journal mode. A file keeps the mode it has, so a refused
// file is not changed at all: it is not switched to WAL on the way to the refusal. Opening
// a WAL database creates its -wal and -shm, as any open does, and the close removes them
// again when this is the last connection.
//
// Read function: call it on the returned Store, outside a transaction.
func OpenExisting(path string) (*Store, error) {
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return nil, fmt.Errorf("store: load migrations: %w", err)
	}
	latest := migrations[len(migrations)-1].version
	db, err := sql.Open("sqlite", path+"?"+readDSNOptions)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := connect(db); err != nil {
		return nil, errors.Join(fmt.Errorf("store: open %s: %w", path, err), db.Close())
	}
	ctx := context.Background()
	var hasTable int
	if err := db.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&hasTable); err != nil {
		return nil, errors.Join(fmt.Errorf("store: read %s: %w", path, err), db.Close())
	}
	if hasTable == 0 {
		return nil, errors.Join(fmt.Errorf("store: %s is not a Cairn database: it has no schema version", path), db.Close())
	}
	v, err := currentVersion(ctx, db)
	if err != nil {
		return nil, errors.Join(err, db.Close())
	}
	if v == 0 {
		return nil, errors.Join(fmt.Errorf("store: %s is not a Cairn database: it has no schema version", path), db.Close())
	}
	if v > latest {
		return nil, errors.Join(fmt.Errorf("store: %s is at schema version %d, newer than this build (%d)", path, v, latest), db.Close())
	}
	return &Store{db: db}, nil
}

// Version returns the schema version the database is at: the highest migration recorded.
//
// Read function: call it on s, outside a transaction.
func (s *Store) Version(ctx context.Context) (int, error) {
	return currentVersion(ctx, s.db)
}

// Backup writes a consistent copy of the database to out. out must be an empty file the
// caller has just made with O_EXCL: SQLite's VACUUM INTO writes into an empty file, and
// refuses any other. The file keeps the mode the caller gave it.
//
// Why VACUUM INTO and not the online backup API: VACUUM INTO runs as one read transaction
// over the whole database. In WAL mode a reader sees the database as it was when its
// statement began, so the copy holds every commit that finished before the call, no commit
// in part, and no later one. A writer is never blocked by it, and it is never blocked by a
// writer. The online backup API restarts when the source changes under it, which a busy
// server does all the time, and it needs the page-level plumbing the driver does not
// otherwise expose. The copy is also a fresh, vacuumed file.
//
// It writes the copy to out and changes nothing in the database.
func (s *Store) Backup(ctx context.Context, out string) error {
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", out); err != nil {
		return fmt.Errorf("store: back up to %s: %w", out, err)
	}
	return nil
}

// IntegrityCheck runs PRAGMA integrity_check and returns an error that carries its first
// findings when the result is anything but the single line "ok".
//
// Read function: call it on s, outside a transaction.
func (s *Store) IntegrityCheck(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("store: integrity check: %w", err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return fmt.Errorf("store: integrity check: %w", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: integrity check: %w", err)
	}
	if len(lines) == 1 && lines[0] == "ok" {
		return nil
	}
	if len(lines) > 3 {
		lines = append(lines[:3], "...")
	}
	return fmt.Errorf("store: integrity check failed: %s", strings.Join(lines, "; "))
}

// ExclusiveLock is an exclusive hold on a database file. While it is held, no other
// connection can read or write the database. Release gives it up.
type ExclusiveLock struct {
	db   *sql.DB
	conn *sql.Conn
}

// CheckReadable reports whether the file at path can be read as an SQLite database with a schema.
// It returns an error wrapping ErrNotDatabase when it cannot, and nil when it can. The file is
// opened read-only, and that matters: on the close of a read-write open of a file it cannot read,
// SQLite deletes the -wal beside it, and a restore must never be the one that deletes a -wal. A
// read-only open leaves the -wal as it is, and it may create an empty -shm beside an unreadable
// file.
func CheckReadable(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", "file:"+uriPath(path)+"?mode=ro&_busy_timeout="+exclusiveBusyTimeout)
	if err != nil {
		return fmt.Errorf("store: open %s: %w", path, err)
	}
	var n int
	qerr := db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&n)
	cerr := db.Close()
	if qerr != nil {
		return errors.Join(probeError(qerr, path), cerr)
	}
	return cerr
}

// uriPath escapes the characters of a file path that a file: URI reads as its own syntax, so the
// path is opened as it is written.
func uriPath(path string) string {
	return strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
}

// AcquireExclusive takes the exclusive lock on the database at path, which must exist. It
// returns ErrInUse when another connection has the database open: an idle server holds it
// too, and so does a server that is writing. It returns ErrNotDatabase when the file cannot
// be read as a database at all.
//
// The lock is taken with locking_mode=EXCLUSIVE and a read, not with BEGIN EXCLUSIVE alone.
// In WAL mode BEGIN EXCLUSIVE is the same as BEGIN IMMEDIATE, which an idle reader does not
// block, so it would let a running server through. The exclusive locking mode needs every
// other connection to have released the database's shared memory, and the read is what
// asks for it. internal/store's tests check the probe against an open idle store, against
// a store with a writer running, and against a closed one.
func AcquireExclusive(ctx context.Context, path string) (*ExclusiveLock, error) {
	db, err := sql.Open("sqlite", path+"?_busy_timeout="+exclusiveBusyTimeout)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, errors.Join(probeError(err, path), db.Close())
	}
	steps := []string{"PRAGMA locking_mode=EXCLUSIVE", "BEGIN EXCLUSIVE"}
	for _, q := range steps {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			return nil, errors.Join(probeError(err, path), conn.Close(), db.Close())
		}
	}
	var n int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&n); err != nil {
		return nil, errors.Join(probeError(err, path), rollbackAndClose(ctx, conn), db.Close())
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return nil, errors.Join(probeError(err, path), conn.Close(), db.Close())
	}
	return &ExclusiveLock{db: db, conn: conn}, nil
}

// Consolidate makes the database file hold every commit on its own. It folds the -wal into
// the file with a full checkpoint, then switches the database to the DELETE journal mode,
// which removes the -wal and -shm. It must run while the lock is held and before the file
// is moved. A checkpoint that is not complete is an error: busy must be 0, and the frames
// logged must equal the frames checkpointed. A database that is not in WAL mode reports
// -1 for both, which is complete, as there is nothing to fold.
//
// It changes the file's contents only by writing the -wal's commits into it, which makes no
// difference to any reader.
func (l *ExclusiveLock) Consolidate(ctx context.Context) error {
	var busy, logged, done int
	if err := l.conn.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &logged, &done); err != nil {
		return fmt.Errorf("store: checkpoint: %w", err)
	}
	if busy != 0 || logged != done {
		return fmt.Errorf("store: checkpoint incomplete: busy %d, %d frames logged, %d checkpointed", busy, logged, done)
	}
	var mode string
	if err := l.conn.QueryRowContext(ctx, "PRAGMA journal_mode=DELETE").Scan(&mode); err != nil {
		return fmt.Errorf("store: switch the journal mode to DELETE: %w", err)
	}
	if mode != "delete" {
		return fmt.Errorf("store: switch the journal mode to DELETE: the database reports %q", mode)
	}
	return nil
}

// Release gives the lock up by closing the probe connection. The database is not written
// by the close: Consolidate has already left it in the DELETE mode, with no -wal to fold.
func (l *ExclusiveLock) Release() error {
	return errors.Join(l.conn.Close(), l.db.Close())
}

// probeError classifies an error from the probe: SQLITE_BUSY is ErrInUse, a file that is not a
// database or is malformed is ErrNotDatabase, and any other error is returned with the path.
func probeError(err error, path string) error {
	switch {
	case isBusy(err):
		return fmt.Errorf("%s: %w", path, ErrInUse)
	case resultCode(err, sqliteNotADB), resultCode(err, sqliteCorrupt):
		return fmt.Errorf("%s: %w: %v", path, ErrNotDatabase, err)
	}
	return fmt.Errorf("store: lock %s: %w", path, err)
}

// rollbackAndClose rolls back the open transaction of the probe, so its connection can be
// closed, and returns the error of the rollback, if any, for the caller to join.
func rollbackAndClose(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, "ROLLBACK")
	return errors.Join(err, conn.Close())
}
