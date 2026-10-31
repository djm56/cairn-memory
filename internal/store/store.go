// Package store is Cairn's SQLite layer. It opens the local database, applies
// the numbered schema migrations (docs/DESIGN.md §3), and holds the typed
// queries for every table, the WithTx write transaction, and the scoped search
// over search_index. It holds no rules: status transitions, the close policy
// and identity live in internal/core.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	// Pure-Go SQLite driver, named in docs/DESIGN.md §3. It needs no C toolchain.
	_ "modernc.org/sqlite"
)

// sqliteBusy is the primary result code SQLITE_BUSY: the database is locked by
// another connection.
const sqliteBusy = 5

// connectRetries is how many times connect tries the first connection when it
// reports SQLITE_BUSY. The waits grow by 10 ms each time.
const connectRetries = 10

// dsnOptions are applied to every connection in the pool, not only the first.
//
//   - _busy_timeout: a writer waits up to 5 s for a lock before SQLITE_BUSY,
//     so several processes sharing one file retry rather than fail.
//   - _foreign_keys: SQLite leaves foreign keys off unless asked, per connection.
//   - _journal_mode=WAL: readers do not block the writer, and several MCP
//     processes can share the file (docs/DESIGN.md §5.3).
//   - _txlock=immediate: every transaction takes the write lock when it begins.
//     A deferred transaction that reads and then writes can fail at once with
//     SQLITE_BUSY in WAL mode, ignoring the busy timeout, so it is avoided.
const dsnOptions = "_busy_timeout=5000&_foreign_keys=1&_journal_mode=WAL&_txlock=immediate"

// Store is an open Cairn database, with its schema at the current version.
// Use it from many goroutines: the underlying pool is safe for concurrent use.
//
// Typical use: s, err := store.Open(path); defer s.Close().
type Store struct {
	db *sql.DB
}

// Open opens the SQLite database at path, creating the file if it does not
// exist, and applies every migration it has not yet run. A database newer than
// this build is refused rather than opened.
//
// path is a filesystem path. It must not contain '?', which starts the driver's
// query string. Open first connects with a retry on SQLITE_BUSY (see connect), so
// several processes opening one new file at once should succeed within that
// retry budget. Open returns an error when
// the file cannot be opened, when a migration fails (the database is then left at
// the last version that applied), or when the schema is newer than this build.
// Open writes to the file.
//
// When path is ":memory:", Open limits the pool to one connection, so every call
// on the Store shares one database. Without the limit each pooled connection
// would open its own empty database. The cost is that the one connection is
// shared: a call on the Store waits while a WithTx holds it, and a call on the
// Store made inside WithTx waits forever. Inside WithTx, use the Querier that
// WithTx passes in.
func Open(path string) (*Store, error) {
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return nil, fmt.Errorf("store: load migrations: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?"+dsnOptions)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if path == ":memory:" {
		// Each pooled connection to ":memory:" is a separate, empty database.
		// One connection makes every call on the Store see the same data.
		db.SetMaxOpenConns(1)
	}
	if err := connect(db); err != nil {
		// The close error is joined, as on the migrate path below.
		return nil, errors.Join(fmt.Errorf("store: open %s: %w", path, err), db.Close())
	}
	if err := migrate(db, migrations); err != nil {
		// The close error is joined, not dropped. errors.Join ignores a nil one.
		return nil, errors.Join(fmt.Errorf("store: migrate %s: %w", path, err), db.Close())
	}
	return &Store{db: db}, nil
}

// connect makes the first connection to db and returns once it is set up. Its
// setup switches a new file to WAL mode, which SQLite can refuse with
// SQLITE_BUSY even with the busy timeout set when another process is doing the
// same switch. A refused connection is discarded and nothing is left behind, so
// connect tries again. Any other error, or a BUSY that persists past the retries,
// is returned.
//
// The retry budget is about 450 ms in all (nine waits of 10 to 90 ms), which is
// deliberately short next to the 5 s busy timeout. The switch is brief, so a
// refusal that outlasts the budget means something else holds the file. Open
// then returns the error rather than block for seconds on a switch that the
// busy handler does not cover anyway.
func connect(db *sql.DB) error {
	var err error
	for i := 0; i < connectRetries; i++ {
		if err = db.Ping(); err == nil || !isBusy(err) {
			return err
		}
		if i < connectRetries-1 {
			time.Sleep(time.Duration(i+1) * 10 * time.Millisecond)
		}
	}
	return err
}

// isBusy reports whether err carries SQLITE_BUSY. The driver's error, a
// *sqlite.Error from modernc.org/sqlite, exposes its result code through Code.
// The primary code is the low byte, so extended codes such as SQLITE_BUSY_SNAPSHOT
// also count.
func isBusy(err error) bool {
	return resultCode(err, sqliteBusy)
}

// resultCode reports whether err carries the primary result code code. The driver's error
// exposes its result code through Code, and the primary code is the low byte, as isBusy uses.
func resultCode(err error, code int) bool {
	var coded interface{ Code() int }
	return errors.As(err, &coded) && coded.Code()&0xff == code
}

// Close releases the database and its connections. It writes nothing to the
// file. Do not use the Store after Close.
func (s *Store) Close() error {
	return s.db.Close()
}
