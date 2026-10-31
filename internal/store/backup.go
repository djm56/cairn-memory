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

// exclusiveBusyTimeout is how long AcquireExclusive waits for a connection that holds the
// database. It is short: a busy database is refused at once, and the caller says so.
const exclusiveBusyTimeout = "250"

// OpenExisting opens the database at path without applying migrations. The file must
// exist and hold a Cairn schema: a file with no schema version is refused as not a Cairn
// database, and one whose version is newer than this build's migrations is refused as
// the schema of a newer build. The backup and restore paths use it, so a backup is never
// changed to a newer schema on the way out, and a backup is verified without being
// migrated. It writes nothing that a read would not, apart from the journal mode the
// connection sets, which a Cairn database already has.
//
// Read function: call it on the returned Store, outside a transaction.
func OpenExisting(path string) (*Store, error) {
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return nil, fmt.Errorf("store: load migrations: %w", err)
	}
	latest := migrations[len(migrations)-1].version
	db, err := sql.Open("sqlite", path+"?"+dsnOptions)
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

// AcquireExclusive takes the exclusive lock on the database at path, which must exist. It
// returns ErrInUse when another connection has the database open, an idle server included.
//
// The lock is taken with locking_mode=EXCLUSIVE and a read, not with BEGIN EXCLUSIVE alone.
// In WAL mode BEGIN EXCLUSIVE is the same as BEGIN IMMEDIATE, which an idle reader does not
// block, so it would let a running server through. The exclusive locking mode needs every
// other connection to have released the database's shared memory, and the read is what
// asks for it. The probe is checked against an open, idle server in internal/store's tests.
func AcquireExclusive(ctx context.Context, path string) (*ExclusiveLock, error) {
	db, err := sql.Open("sqlite", path+"?_busy_timeout="+exclusiveBusyTimeout)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, errors.Join(busyOr(err, path), db.Close())
	}
	steps := []string{"PRAGMA locking_mode=EXCLUSIVE", "BEGIN EXCLUSIVE"}
	for _, q := range steps {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			return nil, errors.Join(busyOr(err, path), conn.Close(), db.Close())
		}
	}
	var n int
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&n); err != nil {
		return nil, errors.Join(busyOr(err, path), rollbackAndClose(ctx, conn), db.Close())
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return nil, errors.Join(busyOr(err, path), conn.Close(), db.Close())
	}
	return &ExclusiveLock{db: db, conn: conn}, nil
}

// Release gives the lock up by closing the probe connection. Closing the last connection
// lets SQLite checkpoint and remove the database's -wal and -shm, so the lock and the
// files' state are released together.
func (l *ExclusiveLock) Release() error {
	return errors.Join(l.conn.Close(), l.db.Close())
}

// busyOr returns ErrInUse, wrapped with the path, when err is SQLITE_BUSY; any other error is
// returned as it is, with the path.
func busyOr(err error, path string) error {
	if isBusy(err) {
		return fmt.Errorf("%s: %w", path, ErrInUse)
	}
	return fmt.Errorf("store: lock %s: %w", path, err)
}

// rollbackAndClose rolls back the open transaction of the probe, so its connection can be
// closed, and returns the error of the rollback, if any, for the caller to join.
func rollbackAndClose(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, "ROLLBACK")
	return errors.Join(err, conn.Close())
}
