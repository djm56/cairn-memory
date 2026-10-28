// Package store is Cairn's SQLite layer. It opens the local database, applies
// the numbered schema migrations (docs/DESIGN.md §3), and holds the typed
// queries for every table, the WithTx write transaction, and the scoped search
// over search_index. It holds no rules: status transitions, the close policy
// and identity live in internal/core.
package store

import (
	"database/sql"
	"fmt"

	// Pure-Go SQLite driver, named in docs/DESIGN.md §3. It needs no C toolchain.
	_ "modernc.org/sqlite"
)

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
// query string. Open returns an error when the file cannot be opened, when a
// migration fails (the database is then left at the last version that applied),
// or when the schema is newer than this build. Open writes to the file.
func Open(path string) (*Store, error) {
	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		return nil, fmt.Errorf("store: load migrations: %w", err)
	}
	db, err := sql.Open("sqlite", path+"?"+dsnOptions)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	if err := migrate(db, migrations); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close releases the database and its connections. It writes nothing to the
// file. Do not use the Store after Close.
func (s *Store) Close() error {
	return s.db.Close()
}
