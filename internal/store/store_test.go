package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// openTemp opens a fresh database in the test's temporary directory and
// registers its close with the test.
func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "cairn.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// pragmaString reads a PRAGMA that returns one text value.
func pragmaString(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var v string
	if err := db.QueryRow("PRAGMA " + name).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return v
}

// TestOpenSetsConnectionOptions checks the three settings that every
// connection in the pool needs: WAL, the busy timeout and foreign keys.
func TestOpenSetsConnectionOptions(t *testing.T) {
	s := openTemp(t)
	if got := pragmaString(t, s.db, "journal_mode"); got != "wal" {
		t.Errorf("journal_mode = %q, want wal", got)
	}
	if got := pragmaString(t, s.db, "busy_timeout"); got != "5000" {
		t.Errorf("busy_timeout = %q, want 5000", got)
	}
	if got := pragmaString(t, s.db, "foreign_keys"); got != "1" {
		t.Errorf("foreign_keys = %q, want 1", got)
	}
}

// TestForeignKeysAreEnforced checks that the declared references refuse an
// orphan row. A setting that is on but enforces nothing would pass the test
// above, so this one checks behaviour.
func TestForeignKeysAreEnforced(t *testing.T) {
	s := openTemp(t)
	_, err := s.db.Exec(`INSERT INTO tasks (id, project, title, goal, status, priority,
		opened_at, updated_at) VALUES ('task-20261028-01', 'no-such-project',
		't', 'g', 'open', 'normal', 'now', 'now')`)
	if err == nil {
		t.Fatal("insert of a task with an unknown project succeeded")
	}
}

// TestEnumCheckRefusesUnknownValue checks that a status outside the DESIGN §3
// list is refused by the schema, not only by core.
func TestEnumCheckRefusesUnknownValue(t *testing.T) {
	s := openTemp(t)
	if _, err := s.db.Exec(`INSERT INTO projects (name, created_at, last_seen_at)
		VALUES ('acme-api', 'now', 'now')`); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	_, err := s.db.Exec(`INSERT INTO tasks (id, project, title, goal, status, priority,
		opened_at, updated_at) VALUES ('task-20261028-01', 'acme-api',
		't', 'g', 'whenever', 'normal', 'now', 'now')`)
	if err == nil {
		t.Fatal("insert with status 'whenever' succeeded")
	}
}

// TestReopenAppliesNothing opens a database, closes it and opens it again.
// The second open must record no new migration and leave the first rows
// untouched.
func TestReopenAppliesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cairn.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	before := migrationRows(t, s.db)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()
	after := migrationRows(t, s2.db)
	if len(before) == 0 {
		t.Fatal("no migrations recorded on first open")
	}
	if len(after) != len(before) {
		t.Fatalf("reopen changed the migration rows: before %v, after %v", before, after)
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("row %d changed on reopen: before %q, after %q", i, before[i], after[i])
		}
	}
}

// migrationRows returns each schema_migrations row as "version|name|applied_at",
// in order, so two reads can be compared exactly.
func migrationRows(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT version, name, applied_at FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v int
		var name, at string
		if err := rows.Scan(&v, &name, &at); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, fmt.Sprintf("%d|%s|%s", v, name, at))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// TestMigrationsCreateEveryTableAndIndex checks that an empty database ends
// with every table in DESIGN §3, the FTS index, the triggers, and the unique
// directive code.
func TestMigrationsCreateEveryTableAndIndex(t *testing.T) {
	s := openTemp(t)
	wantTables := []string{
		"projects", "sessions", "tasks", "task_criteria", "task_files",
		"task_lessons", "task_mistakes", "steps", "events", "reviews",
		"learnings", "directives", "notes", "api_keys", "search_index",
		"schema_migrations",
	}
	for _, name := range wantTables {
		var n int
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
			t.Fatalf("query %s: %v", name, err)
		}
		if n != 1 {
			t.Errorf("table %s missing", name)
		}
	}

	var fts string
	if err := s.db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE name = 'search_index'`).Scan(&fts); err != nil {
		t.Fatalf("search_index definition: %v", err)
	}
	if !strings.Contains(strings.ToLower(fts), "fts5") {
		t.Errorf("search_index is not an FTS5 table: %s", fts)
	}

	var triggers int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'trigger'`).Scan(&triggers); err != nil {
		t.Fatalf("count triggers: %v", err)
	}
	// Seven source tables, each with insert, update and delete triggers (21),
	// and the tasks project guard from migration 0002.
	if triggers != 22 {
		t.Errorf("trigger count = %d, want 22", triggers)
	}

	// PRAGMA index_list returns seq, name, unique, origin and partial.
	var unique int
	rows, err := s.db.Query(`PRAGMA index_list(directives)`)
	if err != nil {
		t.Fatalf("index_list: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var seq, isUnique, partial int
		var name, origin string
		if err := rows.Scan(&seq, &name, &isUnique, &origin, &partial); err != nil {
			t.Fatalf("scan index_list: %v", err)
		}
		if isUnique == 1 {
			unique++
		}
	}
	if unique == 0 {
		t.Error("directives has no unique index (directives.code must be UNIQUE)")
	}
}

// TestMigration0002IndexesAndGuard checks that every index migration 0002
// creates exists under its name, and that the tasks.project guard trigger is
// in place. The list is the migration's own, so a dropped or renamed index
// fails here rather than as a silent slow query.
func TestMigration0002IndexesAndGuard(t *testing.T) {
	s := openTemp(t)
	objects := []struct{ kind, name string }{
		{"index", "idx_steps_task"},
		{"index", "idx_events_task"},
		{"index", "idx_events_step"},
		{"index", "idx_reviews_task"},
		{"index", "idx_reviews_step"},
		{"index", "idx_learnings_project_status"},
		{"index", "idx_notes_task"},
		{"index", "idx_sessions_project"},
		{"index", "idx_tasks_project_updated"},
		{"trigger", "tasks_project_immutable"},
	}
	for _, o := range objects {
		var n int
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`, o.kind, o.name).Scan(&n); err != nil {
			t.Fatalf("query %s %s: %v", o.kind, o.name, err)
		}
		if n != 1 {
			t.Errorf("%s %s missing from sqlite_master", o.kind, o.name)
		}
	}
}

// TestMigrationsRecordVersions checks that a fresh database ends at version 2,
// with one row per migration, each recorded by its number and name.
func TestMigrationsRecordVersions(t *testing.T) {
	s := openTemp(t)
	rows := migrationRows(t, s.db)
	want := []string{"1|schema_v1|", "2|indexes_and_guards|"}
	if len(rows) != len(want) {
		t.Fatalf("migration rows = %v, want %d", rows, len(want))
	}
	for i, prefix := range want {
		if !strings.HasPrefix(rows[i], prefix) {
			t.Errorf("migration row %d = %q, want prefix %q", i, rows[i], prefix)
		}
	}
}

// TestFailedMigrationLeavesPreviousVersion runs two migrations, the second of
// which fails. The first must stay applied and the failed one must roll back
// completely, so the database is at version 1.
func TestFailedMigrationLeavesPreviousVersion(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cairn.db")+"?"+dsnOptions)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	list := []migration{
		{version: 1, name: "first", sql: `CREATE TABLE first_table (id INTEGER PRIMARY KEY);`},
		{version: 2, name: "broken", sql: `CREATE TABLE second_table (id INTEGER PRIMARY KEY);
			THIS IS NOT SQL;`},
	}
	if err := migrate(db, list); err == nil {
		t.Fatal("migrate succeeded with a broken migration")
	}

	var v int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if v != 1 {
		t.Errorf("version after failure = %d, want 1", v)
	}
	if !tableExists(t, db, "first_table") {
		t.Error("first_table (version 1) was rolled back with the failed migration")
	}
	if tableExists(t, db, "second_table") {
		t.Error("second_table exists: the failed migration was not rolled back")
	}
}

// TestMigrateRefusesNewerDatabase checks that a database at a version this
// build does not know is refused, not opened and left alone.
func TestMigrateRefusesNewerDatabase(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cairn.db")+"?"+dsnOptions)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	full := []migration{
		{version: 1, name: "one", sql: `CREATE TABLE a (id INTEGER);`},
		{version: 2, name: "two", sql: `CREATE TABLE b (id INTEGER);`},
	}
	if err := migrate(db, full); err != nil {
		t.Fatalf("migrate to 2: %v", err)
	}
	if err := migrate(db, full[:1]); err == nil {
		t.Error("migrate to 1 accepted a database at version 2")
	}
}

// TestLoadMigrationsRejectsGap checks that a missing version is refused when
// the migration files are loaded.
func TestLoadMigrationsRejectsGap(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0001_one.sql":   {Data: []byte("SELECT 1;")},
		"migrations/0003_three.sql": {Data: []byte("SELECT 1;")},
	}
	if _, err := loadMigrations(fsys); err == nil {
		t.Error("loadMigrations accepted versions 1 and 3 with 2 missing")
	}
}

// TestLoadMigrationsEmbedsAll checks that the embedded set is exactly the two
// schema migrations, in order.
func TestLoadMigrationsEmbedsAll(t *testing.T) {
	list, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	want := []struct {
		version int
		name    string
	}{{1, "schema_v1"}, {2, "indexes_and_guards"}}
	if len(list) != len(want) {
		t.Fatalf("embedded migrations = %d, want %d", len(list), len(want))
	}
	for i, w := range want {
		if list[i].version != w.version || list[i].name != w.name {
			t.Errorf("migration %d = %d_%s, want %d_%s", i, list[i].version, list[i].name, w.version, w.name)
		}
	}
}

// tableExists reports whether a table with the given name is in the database.
func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil {
		t.Fatalf("table %s: %v", name, err)
	}
	return n == 1
}
