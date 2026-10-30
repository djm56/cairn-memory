package store

import (
	"context"
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

// TestEnumCheckRefusesUnknownValue checks that a value outside each enumerated
// column's list in DESIGN §3 is refused by the schema, not only by core. There is
// one case per CHECK in the migrations, so a dropped or narrowed CHECK fails here.
// Each case runs on a fresh database seeded with a project, a task and a step,
// and the refusal must be a CHECK failure, not some other constraint.
func TestEnumCheckRefusesUnknownValue(t *testing.T) {
	seed := []string{
		`INSERT INTO projects (name, created_at, last_seen_at) VALUES ('acme-api', 'now', 'now')`,
		`INSERT INTO tasks (id, project, title, goal, status, priority, opened_at, updated_at)
			VALUES ('task-20261028-01', 'acme-api', 't', 'g', 'open', 'normal', 'now', 'now')`,
		`INSERT INTO steps (task_id, position, title, status)
			VALUES ('task-20261028-01', 1, 's', 'pending')`,
	}
	cases := []struct {
		column string // the CHECK's column, as in the migration
		insert string // an insert whose one enumerated value is not in the list
	}{
		{"tasks.status", `INSERT INTO tasks (id, project, title, goal, status, priority, opened_at, updated_at)
			VALUES ('task-20261028-02', 'acme-api', 't', 'g', 'whenever', 'normal', 'now', 'now')`},
		{"tasks.priority", `INSERT INTO tasks (id, project, title, goal, status, priority, opened_at, updated_at)
			VALUES ('task-20261028-02', 'acme-api', 't', 'g', 'open', 'whenever', 'now', 'now')`},
		{"task_criteria.result", `INSERT INTO task_criteria (task_id, ord, criterion, result)
			VALUES ('task-20261028-01', 1, 'c', 'whenever')`},
		{"task_files.role", `INSERT INTO task_files (task_id, role, ord, path)
			VALUES ('task-20261028-01', 'whenever', 1, 'p')`},
		{"task_lessons.kind", `INSERT INTO task_lessons (task_id, ord, kind, text)
			VALUES ('task-20261028-01', 1, 'whenever', 't')`},
		{"task_mistakes.impact", `INSERT INTO task_mistakes (task_id, ord, agent, mistake, impact)
			VALUES ('task-20261028-01', 1, 'main', 'm', 'whenever')`},
		{"steps.status", `INSERT INTO steps (task_id, position, title, status)
			VALUES ('task-20261028-01', 2, 's', 'whenever')`},
		{"events.kind", `INSERT INTO events (task_id, project, agent, kind, text, occurred_at)
			VALUES ('task-20261028-01', 'acme-api', 'main', 'whenever', 't', 'now')`},
		{"reviews.verdict", `INSERT INTO reviews (task_id, step_id, reviewer, verdict, critical, warnings, created_at)
			VALUES ('task-20261028-01', 1, 'apone', 'whenever', 0, 0, 'now')`},
		{"learnings.kind", `INSERT INTO learnings (project, agent, kind, title, body, status, created_at)
			VALUES ('acme-api', 'main', 'whenever', 't', 'b', 'proposed', 'now')`},
		{"learnings.status", `INSERT INTO learnings (project, agent, kind, title, body, status, created_at)
			VALUES ('acme-api', 'main', 'finding', 't', 'b', 'whenever', 'now')`},
		{"directives.scope", `INSERT INTO directives (code, scope, title, rule, status)
			VALUES ('D-0001', 'whenever', 't', 'r', 'active')`},
		{"directives.status", `INSERT INTO directives (code, scope, title, rule, status)
			VALUES ('D-0001', 'global', 't', 'r', 'whenever')`},
	}
	for _, c := range cases {
		t.Run(c.column, func(t *testing.T) {
			s := openTemp(t)
			for _, q := range seed {
				if _, err := s.db.Exec(q); err != nil {
					t.Fatalf("seed: %v", err)
				}
			}
			_, err := s.db.Exec(c.insert)
			if err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
				t.Errorf("%s: err = %v, want a CHECK constraint refusal", c.column, err)
			}
		})
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
	// the tasks project guard from migration 0002, and the events project check
	// from migration 0003.
	if triggers != 23 {
		t.Errorf("trigger count = %d, want 23", triggers)
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
	// idx_events_project is not in migration 0002: idx_events_task serves the
	// task_id part of ListEvents. Its absence is asserted so it cannot return
	// silently.
	var dropped int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_events_project'`).Scan(&dropped); err != nil {
		t.Fatalf("query idx_events_project: %v", err)
	}
	if dropped != 0 {
		t.Errorf("index idx_events_project present; migration 0002 does not create it")
	}
}

// TestMigrationsRecordVersions checks that a fresh database ends at version 3,
// with one row per migration, each recorded by its number and name.
func TestMigrationsRecordVersions(t *testing.T) {
	s := openTemp(t)
	rows := migrationRows(t, s.db)
	want := []string{"1|schema_v1|", "2|indexes_and_guards|", "3|events_project_guard|"}
	if len(rows) != len(want) {
		t.Fatalf("migration rows = %v, want %d", rows, len(want))
	}
	for i, prefix := range want {
		if !strings.HasPrefix(rows[i], prefix) {
			t.Errorf("migration row %d = %q, want prefix %q", i, rows[i], prefix)
		}
	}
}

// TestFailedMigrationLeavesPreviousVersion runs a migration list in which one
// migration fails. Each migration before it must stay applied, the failed one
// must roll back completely, and the database must be left at the version before
// it. Each case runs on a fresh database.
func TestFailedMigrationLeavesPreviousVersion(t *testing.T) {
	one := migration{version: 1, name: "first", sql: `CREATE TABLE first_table (id INTEGER PRIMARY KEY);`}
	two := migration{version: 2, name: "second", sql: `CREATE TABLE second_table (id INTEGER PRIMARY KEY);`}
	cases := []struct {
		name        string
		list        []migration
		wantVersion int
		wantTables  []string // present after the failure
		goneTables  []string // absent after the failure
	}{
		{
			name: "broken second migration",
			list: []migration{one, {version: 2, name: "broken", sql: `CREATE TABLE broken_table (id INTEGER PRIMARY KEY);
				THIS IS NOT SQL;`}},
			wantVersion: 1,
			wantTables:  []string{"first_table"},
			goneTables:  []string{"broken_table"},
		},
		{
			name: "broken first migration",
			list: []migration{{version: 1, name: "broken", sql: `CREATE TABLE first_table (id INTEGER PRIMARY KEY);
				THIS IS NOT SQL;`}},
			wantVersion: 0,
			goneTables:  []string{"first_table"},
		},
		{
			name: "broken third migration",
			list: []migration{one, two, {version: 3, name: "broken", sql: `CREATE TABLE third_table (id INTEGER PRIMARY KEY);
				THIS IS NOT SQL;`}},
			wantVersion: 2,
			wantTables:  []string{"first_table", "second_table"},
			goneTables:  []string{"third_table"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cairn.db")+"?"+dsnOptions)
			if err != nil {
				t.Fatalf("sql.Open: %v", err)
			}
			defer db.Close()
			if err := migrate(db, c.list); err == nil {
				t.Fatal("migrate succeeded with a broken migration")
			}
			v, err := currentVersion(context.Background(), db)
			if err != nil {
				t.Fatalf("read version: %v", err)
			}
			if v != c.wantVersion {
				t.Errorf("version after failure = %d, want %d", v, c.wantVersion)
			}
			for _, name := range c.wantTables {
				if !tableExists(t, db, name) {
					t.Errorf("%s was rolled back with the failed migration", name)
				}
			}
			for _, name := range c.goneTables {
				if tableExists(t, db, name) {
					t.Errorf("%s exists: the failed migration was not rolled back", name)
				}
			}
		})
	}
}

// TestMigrateRefusesNewerDatabase checks migrate against the version the database
// is at. A build at that version or above opens it. A build older than the
// database is refused, and the database stays at its version. Each case runs on a
// fresh database.
func TestMigrateRefusesNewerDatabase(t *testing.T) {
	full := []migration{
		{version: 1, name: "one", sql: `CREATE TABLE a (id INTEGER);`},
		{version: 2, name: "two", sql: `CREATE TABLE b (id INTEGER);`},
	}
	cases := []struct {
		name        string
		applied     []migration // applied first, to set the database's version
		build       []migration // the build's list, then migrated to
		wantErr     bool
		wantVersion int
	}{
		{"build at the database version", full, full, false, 2},
		{"build newer than the database", full[:1], full, false, 2},
		{"build older than the database", full, full[:1], true, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cairn.db")+"?"+dsnOptions)
			if err != nil {
				t.Fatalf("sql.Open: %v", err)
			}
			defer db.Close()
			if err := migrate(db, c.applied); err != nil {
				t.Fatalf("migrate to %d: %v", len(c.applied), err)
			}
			err = migrate(db, c.build)
			if (err != nil) != c.wantErr {
				t.Errorf("migrate err = %v, want an error: %v", err, c.wantErr)
			}
			v, verr := currentVersion(context.Background(), db)
			if verr != nil {
				t.Fatalf("read version: %v", verr)
			}
			if v != c.wantVersion {
				t.Errorf("version = %d, want %d", v, c.wantVersion)
			}
		})
	}
}

// TestLoadMigrationsRejects checks that loadMigrations refuses a set of files it
// cannot run: a name that does not match NNNN_name.sql, two files with one
// version, and a missing version. Each case names the reason the error must give,
// so a refusal for the wrong reason fails the case.
func TestLoadMigrationsRejects(t *testing.T) {
	cases := []struct {
		name    string
		files   map[string]string
		wantErr string // substring of the error
	}{
		{"name without the four-digit number", map[string]string{
			"migrations/1_one.sql": "SELECT 1;"}, "does not match NNNN_name.sql"},
		{"name with a hyphen", map[string]string{
			"migrations/0001-one.sql": "SELECT 1;"}, "does not match NNNN_name.sql"},
		{"name with an upper-case letter", map[string]string{
			"migrations/0001_One.sql": "SELECT 1;"}, "does not match NNNN_name.sql"},
		{"two files with one version", map[string]string{
			"migrations/0001_one.sql": "SELECT 1;",
			"migrations/0001_uno.sql": "SELECT 2;"}, "found version 1 at position 2"},
		{"a missing version", map[string]string{
			"migrations/0001_one.sql":   "SELECT 1;",
			"migrations/0003_three.sql": "SELECT 1;"}, "found version 3 at position 2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := fstest.MapFS{}
			for name, text := range c.files {
				fsys[name] = &fstest.MapFile{Data: []byte(text)}
			}
			_, err := loadMigrations(fsys)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("loadMigrations err = %v, want it to contain %q", err, c.wantErr)
			}
		})
	}
}

// TestLoadMigrationsEmbedsAll checks that the embedded set is exactly the three
// schema migrations, in order.
func TestLoadMigrationsEmbedsAll(t *testing.T) {
	list, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	want := []struct {
		version int
		name    string
	}{{1, "schema_v1"}, {2, "indexes_and_guards"}, {3, "events_project_guard"}}
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
