package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"time"
)

// migrationFiles holds the numbered schema files, embedded in the binary so a
// database can be migrated without the source tree.
//
//go:embed migrations/*.sql
var migrationFiles embed.FS

// migration is one numbered schema change.
type migration struct {
	version int    // 1, 2, ... with no gaps
	name    string // the part of the file name after the number
	sql     string // the file's text; may hold several statements
}

// migrationFile matches a migration file name: four digits, an underscore, then
// a lower-case name, as in 0001_schema_v1.sql.
var migrationFile = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// loadMigrations reads the migrations from the migrations directory of fsys,
// in version order. It returns an error when a file name does not match the
// pattern, when a version appears twice, or when the versions do not run
// 1, 2, 3 with no gap. An empty set is an error.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return nil, err
	}
	var list []migration
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationFile.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migration file %q does not match NNNN_name.sql", e.Name())
		}
		version, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("migration file %q: %w", e.Name(), err)
		}
		text, err := fs.ReadFile(fsys, "migrations/"+e.Name())
		if err != nil {
			return nil, err
		}
		list = append(list, migration{version: version, name: m[2], sql: string(text)})
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no migrations found")
	}
	sort.Slice(list, func(i, j int) bool { return list[i].version < list[j].version })
	for i, m := range list {
		if m.version != i+1 {
			return nil, fmt.Errorf("migrations must run 1..%d with no gap or repeat; found version %d at position %d",
				len(list), m.version, i+1)
		}
	}
	return list, nil
}

// migrate brings the database to the newest version in list. It creates the
// schema_migrations table if needed, in its own transaction, then applies each
// migration above the current version in its own transaction. The DSN sets
// _txlock=immediate, so every transaction takes the write lock when it begins
// and the busy timeout applies. A bare autocommit CREATE TABLE does not: it
// reads first, then upgrades to a write, and in WAL mode SQLite returns
// SQLITE_BUSY at once when another process committed in between. The version is
// read inside each migration transaction, under the write lock, so two processes
// opening one file cannot apply a migration twice. A migration that fails rolls
// back alone: the database is left at the version before it. It returns an error
// when the database is at a version newer than list. Migrations applied earlier
// are not undone.
func migrate(db *sql.DB, list []migration) error {
	if len(list) == 0 {
		return fmt.Errorf("no migrations to apply")
	}
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema_migrations: %w", err)
	}
	defer tx.Rollback() // a no-op once Commit has succeeded
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version     INTEGER PRIMARY KEY,
		name        TEXT NOT NULL,
		applied_at  TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema_migrations: %w", err)
	}
	for _, m := range list {
		if err := applyOne(ctx, db, m); err != nil {
			return err
		}
	}
	latest := list[len(list)-1].version
	current, err := currentVersion(ctx, db)
	if err != nil {
		return err
	}
	if current > latest {
		return fmt.Errorf("database is at schema version %d, newer than this build (%d)", current, latest)
	}
	return nil
}

// applyOne runs m in its own transaction if the database is below its version.
// It returns an error when the database is not exactly one version behind m,
// which means an earlier migration is missing.
func applyOne(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", m.version, err)
	}
	defer tx.Rollback() // a no-op once Commit has succeeded

	var current int
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current >= m.version {
		return nil // already applied; nothing to do
	}
	if current != m.version-1 {
		return fmt.Errorf("database is at version %d; migration %d cannot follow it", current, m.version)
	}
	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("migration %04d_%s: %w", m.version, m.name, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.version, m.name, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("record migration %04d: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %04d: %w", m.version, err)
	}
	return nil
}

// currentVersion returns the highest migration version recorded, or 0 for a
// database with none.
func currentVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v int
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return v, nil
}
