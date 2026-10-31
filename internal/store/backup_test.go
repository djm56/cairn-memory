package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// TestAcquireExclusiveRefusesAnIdleOpenDatabase checks the probe the restore uses: a database that
// another connection has open is ErrInUse, even when that connection is idle, and once it closes the
// lock is taken.
func TestAcquireExclusiveRefusesAnIdleOpenDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cairn.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireExclusive(ctx, path); !errors.Is(err, ErrInUse) {
		t.Fatalf("AcquireExclusive with an open store: err = %v, want ErrInUse", err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireExclusive(ctx, path)
	if err != nil {
		t.Fatalf("AcquireExclusive after the store closed: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatalf("Open after Release: %v", err)
	}
	again.Close()
}

// TestOpenExistingRefusesWhatIsNotACairnDatabase checks that a file with no schema, and one whose
// schema is newer than this build, are refused, and that neither is migrated.
func TestOpenExistingRefusesWhatIsNotACairnDatabase(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.db")
	raw, err := sql.Open("sqlite", plain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	if _, err := OpenExisting(plain); err == nil || !strings.Contains(err.Error(), "not a Cairn database") {
		t.Errorf("OpenExisting of a file with no schema: err = %v, want not a Cairn database", err)
	}

	newer := filepath.Join(dir, "newer.db")
	st, err := Open(newer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO schema_migrations (version, name, applied_at) VALUES (99, 'future', 'x')`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if _, err := OpenExisting(newer); err == nil || !strings.Contains(err.Error(), "newer than this build") {
		t.Errorf("OpenExisting of a newer schema: err = %v, want the newer-than-this-build refusal", err)
	}
}

// TestOpenExistingDoesNotMigrate checks that a database at an older schema is opened as it is:
// OpenExisting does not apply the migrations Open would.
func TestOpenExistingDoesNotMigrate(t *testing.T) {
	list, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path+"?"+dsnOptions)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrate(db, list[:4]); err != nil {
		t.Fatal(err)
	}
	db.Close()

	st, err := OpenExisting(path)
	if err != nil {
		t.Fatalf("OpenExisting of a version 4 database: %v", err)
	}
	defer st.Close()
	if v, err := st.Version(context.Background()); err != nil || v != 4 {
		t.Errorf("Version after OpenExisting = %d (%v), want 4: it must not migrate", v, err)
	}
}

// TestIntegrityCheckPassesAFreshDatabase checks the integrity check on a database the store made.
func TestIntegrityCheckPassesAFreshDatabase(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "cairn.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.IntegrityCheck(context.Background()); err != nil {
		t.Errorf("IntegrityCheck of a fresh database: %v", err)
	}
}
