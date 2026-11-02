package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMigration0005ReviewByBackfill upgrades a database at version 4 that holds
// directives, and checks the backfill of schema v5. An active directive with a
// ratified_at is due 90 days after it. An active one with no ratified_at is due
// 90 days after the upgrade, so the upgrade alone never expires it. A retired
// directive keeps NULL.
func TestMigration0005ReviewByBackfill(t *testing.T) {
	ctx := context.Background()
	list, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cairn.db")+"?"+dsnOptions)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	// Version 4: the schema before review_by existed.
	if err := migrate(db, list[:4]); err != nil {
		t.Fatalf("migrate to v4: %v", err)
	}
	for _, row := range []string{
		`INSERT INTO directives (code, scope, title, rule, status, ratified_by, ratified_at)
			VALUES ('D-0001', 'global', 'ratified', 'r', 'active', 'op', '2026-01-01T00:00:00.000000000Z')`,
		`INSERT INTO directives (code, scope, title, rule, status, ratified_by, ratified_at)
			VALUES ('D-0002', 'global', 'never-ratified-time', 'r', 'active', 'op', NULL)`,
		`INSERT INTO directives (code, scope, title, rule, status, ratified_by, ratified_at, retired_at)
			VALUES ('D-0003', 'global', 'retired', 'r', 'retired', 'op', '2026-01-01T00:00:00.000000000Z', '2026-02-01T00:00:00.000000000Z')`,
	} {
		if _, err := db.ExecContext(ctx, row); err != nil {
			t.Fatalf("seed v4 directive: %v", err)
		}
	}

	before := time.Now().UTC()
	if err := migrate(db, list); err != nil {
		t.Fatalf("migrate to v5: %v", err)
	}
	after := time.Now().UTC()

	reviewBy := func(code string) sql.NullString {
		t.Helper()
		var v sql.NullString
		if err := db.QueryRowContext(ctx, `SELECT review_by FROM directives WHERE code = ?`, code).Scan(&v); err != nil {
			t.Fatalf("read review_by %s: %v", code, err)
		}
		return v
	}

	// 2026-01-01 plus 90 days is 2026-04-01, in the stored form.
	if got := reviewBy("D-0001"); got.String != "2026-04-01T00:00:00.000000000Z" {
		t.Errorf("D-0001 review_by = %q, want 2026-04-01T00:00:00.000000000Z (ratified_at + 90 days)", got.String)
	}

	// No ratified_at: due 90 days after the upgrade ran.
	got := reviewBy("D-0002")
	if !got.Valid {
		t.Fatalf("D-0002 review_by is NULL, want the upgrade time plus 90 days")
	}
	due, err := parseTime(got.String)
	if err != nil {
		t.Fatalf("D-0002 review_by %q is not in the stored form: %v", got.String, err)
	}
	lo, hi := before.AddDate(0, 0, 90).Add(-time.Second), after.AddDate(0, 0, 90).Add(time.Second)
	if due.Before(lo) || due.After(hi) {
		t.Errorf("D-0002 review_by = %v, want between %v and %v", due, lo, hi)
	}

	if got := reviewBy("D-0003"); got.Valid {
		t.Errorf("retired D-0003 review_by = %q, want NULL", got.String)
	}
	if !strings.HasSuffix(reviewBy("D-0001").String, ".000000000Z") {
		t.Errorf("D-0001 review_by is not fixed width: %q", reviewBy("D-0001").String)
	}
}
