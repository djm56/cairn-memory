package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestEveryConnectionHasOptions checks the connection settings on several
// connections held open at once. The DSN applies them per connection, so a
// pragma set on one connection only would pass the single-connection test above
// and fail here.
func TestEveryConnectionHasOptions(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	const n = 3
	conns := make([]*sql.Conn, 0, n)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	for i := 0; i < n; i++ {
		c, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatalf("Conn %d: %v", i, err)
		}
		conns = append(conns, c)
	}
	want := map[string]string{"journal_mode": "wal", "busy_timeout": "5000", "foreign_keys": "1"}
	for i, c := range conns {
		for name, value := range want {
			var got string
			if err := c.QueryRowContext(ctx, "PRAGMA "+name).Scan(&got); err != nil {
				t.Fatalf("conn %d PRAGMA %s: %v", i, name, err)
			}
			if got != value {
				t.Errorf("conn %d %s = %q, want %q", i, name, got, value)
			}
		}
	}
}

// TestOpenConcurrentSameFile starts several Opens on one new file at once. Each
// must succeed, and the file must end with exactly one schema_migrations row per
// migration. The write lock taken by each migration transaction is what stops two
// processes applying the same version twice.
func TestOpenConcurrentSameFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cairn.db")
	const n = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	var opened []*Store
	var errs []error
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Open(path)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			opened = append(opened, s)
		}()
	}
	wg.Wait()
	for _, s := range opened {
		defer s.Close()
	}
	for _, err := range errs {
		t.Errorf("concurrent Open: %v", err)
	}
	if len(opened) == 0 {
		t.FailNow()
	}

	migrations, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	rows := migrationRows(t, opened[0].db)
	if len(rows) != len(migrations) {
		t.Fatalf("schema_migrations rows = %d, want %d: %v", len(rows), len(migrations), rows)
	}
	for i, m := range migrations {
		if want := fmt.Sprintf("%d|%s|", m.version, m.name); !strings.HasPrefix(rows[i], want) {
			t.Errorf("row %d = %q, want prefix %q", i, rows[i], want)
		}
	}
}

// TestApplyOneCases covers the three branches of applyOne: a migration already
// applied is a no-op; the next migration applies; a migration more than one
// version ahead of the database is refused, and its table is not created.
func TestApplyOneCases(t *testing.T) {
	ctx := context.Background()
	one := migration{version: 1, name: "one", sql: `CREATE TABLE a (id INTEGER);`}
	two := migration{version: 2, name: "two", sql: `CREATE TABLE b (id INTEGER);`}

	cases := []struct {
		name    string
		steps   []migration // applied in order, against one fresh database
		wantErr string      // substring of the error for the last step; "" for none
		wantRow int         // schema_migrations rows afterwards
	}{
		{"already applied is a no-op", []migration{one, one}, "", 1},
		{"next version applies", []migration{one, two}, "", 2},
		{"gap is refused", []migration{two}, "cannot follow", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cairn.db")+"?"+dsnOptions)
			if err != nil {
				t.Fatalf("sql.Open: %v", err)
			}
			defer db.Close()
			if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (
				version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
				t.Fatalf("create schema_migrations: %v", err)
			}
			var got error
			for _, m := range c.steps {
				got = applyOne(ctx, db, m)
			}
			if c.wantErr == "" && got != nil {
				t.Fatalf("applyOne: %v", got)
			}
			if c.wantErr != "" && (got == nil || !strings.Contains(got.Error(), c.wantErr)) {
				t.Fatalf("applyOne err = %v, want it to contain %q", got, c.wantErr)
			}
			var rows int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&rows); err != nil {
				t.Fatalf("count: %v", err)
			}
			if rows != c.wantRow {
				t.Errorf("schema_migrations rows = %d, want %d", rows, c.wantRow)
			}
			if c.wantErr != "" && tableExists(t, db, "b") {
				t.Error("table of the refused migration was created")
			}
		})
	}
}

// TestMemoryDatabaseIsShared checks the ":memory:" path. The pool holds one
// connection, so a write made by one call is read by a later call. Without the
// one-connection limit the read could land on a second, empty database.
//
// The wait check holds one row set open, so the pool has no free connection. A
// second query must then wait for that connection and time out. Without the
// one-connection limit it would open a second, empty database and fail at once
// with "no such table", so the timeout is what separates the two.
//
// The result is set by the pool, not by the clock. The rows stay open until the
// test closes them below, after the second query has returned, so with one
// connection that query always waits to its deadline. The 200 ms is only the
// bound on that wait, and the test costs about that long on every run.
func TestMemoryDatabaseIsShared(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err := s.ExecContext(ctx, `INSERT INTO projects (name, created_at, last_seen_at)
		VALUES ('acme-api', 'now', 'now')`); err != nil {
		t.Fatalf("write on one call: %v", err)
	}
	var n int
	if err := s.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&n); err != nil {
		t.Fatalf("read on another call: %v", err)
	}
	if n != 1 {
		t.Errorf("projects = %d on a later call, want 1: the write was not seen", n)
	}

	rows, err := s.QueryContext(ctx, `SELECT name FROM projects`)
	if err != nil {
		t.Fatalf("open rows: %v", err)
	}
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	second, err := s.QueryContext(short, `SELECT COUNT(*) FROM projects`)
	if err == nil {
		// The query did not wait, so it returned rows. The test fails below, and
		// the rows are closed here rather than dropped.
		if cerr := second.Close(); cerr != nil {
			t.Errorf("close rows of the second query: %v", cerr)
		}
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("query while rows are open: err = %v, want a wait that times out", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close rows: %v", err)
	}
	if got := s.db.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1", got)
	}
}
