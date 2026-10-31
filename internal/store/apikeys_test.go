package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// keyAt is an API key row for the tests: a name, a hash and a scope, made at t0.
func keyAt(name, hash, scope string, t0 time.Time) APIKeyNew {
	return APIKeyNew{Name: name, Hash: hash, Scope: scope, CreatedAt: t0}
}

// addKey adds k in its own write transaction and returns its id.
func addKey(t *testing.T, s *Store, k APIKeyNew) int64 {
	t.Helper()
	var id int64
	err := s.WithTx(context.Background(), func(q Querier) error {
		var err error
		id, err = AddAPIKey(context.Background(), q, k)
		return err
	})
	if err != nil {
		t.Fatalf("AddAPIKey %s: %v", k.Name, err)
	}
	return id
}

// TestAPIKeyListHasNoHash checks that a key comes back from the store without its
// hash. The type has no hash field, and the listed rows are printed to be sure no
// hash text is in them.
func TestAPIKeyListHasNoHash(t *testing.T) {
	if _, has := reflect.TypeOf(APIKeyInfo{}).FieldByName("Hash"); has {
		t.Fatal("APIKeyInfo has a Hash field: the store must not return a hash")
	}
	s := openTemp(t)
	t0 := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	addKey(t, s, keyAt("laptop", "deadbeefhash", "agent", t0))
	var list []APIKeyInfo
	if err := s.WithReadTx(context.Background(), func(q Querier) error {
		var err error
		list, err = ListAPIKeys(context.Background(), q)
		return err
	}); err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(list) != 1 || list[0].Name != "laptop" || list[0].Scope != "agent" || !list[0].CreatedAt.Equal(t0) {
		t.Fatalf("list = %+v, want one agent key laptop made at %v", list, t0)
	}
	if strings.Contains(reflect.ValueOf(list).String(), "deadbeefhash") {
		t.Error("the listed rows carry the hash")
	}
}

// TestAPIKeyScopeIsChecked checks that the schema refuses a scope outside the three
// the design names, so a bad scope cannot be stored and then read as a role.
func TestAPIKeyScopeIsChecked(t *testing.T) {
	s := openTemp(t)
	err := s.WithTx(context.Background(), func(q Querier) error {
		_, err := AddAPIKey(context.Background(), q, keyAt("root", "h1", "admin", time.Now()))
		return err
	})
	if err == nil {
		t.Fatal("scope admin was stored: the CHECK constraint must refuse it")
	}
}

// TestAPIKeyNameAndHashAreUnique checks that a second key with a taken name or a
// taken hash is refused, and that a revoked key keeps its name taken.
func TestAPIKeyNameAndHashAreUnique(t *testing.T) {
	s := openTemp(t)
	t0 := time.Now()
	addKey(t, s, keyAt("ci", "h-ci", "agent", t0))
	add := func(k APIKeyNew) error {
		return s.WithTx(context.Background(), func(q Querier) error {
			_, err := AddAPIKey(context.Background(), q, k)
			return err
		})
	}
	if err := add(keyAt("ci", "h-other", "agent", t0)); err == nil {
		t.Error("a second key named ci was stored")
	}
	if err := add(keyAt("other", "h-ci", "agent", t0)); err == nil {
		t.Error("a second key with the same hash was stored")
	}
	if err := s.WithTx(context.Background(), func(q Querier) error {
		return RevokeAPIKey(context.Background(), q, "ci", t0)
	}); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
	if err := add(keyAt("ci", "h-again", "agent", t0)); err == nil {
		t.Error("a name of a revoked key was reused: the row must keep the name")
	}
}

// TestFindActiveAPIKey checks that the active key with a hash is found, and that a
// revoked key and an unknown hash are both not found.
func TestFindActiveAPIKey(t *testing.T) {
	s := openTemp(t)
	t0 := time.Now()
	id := addKey(t, s, keyAt("ops", "h-ops", "operator", t0))
	find := func(hash string) (APIKeyInfo, error) {
		var k APIKeyInfo
		err := s.WithReadTx(context.Background(), func(q Querier) error {
			var err error
			k, err = FindActiveAPIKey(context.Background(), q, hash)
			return err
		})
		return k, err
	}
	k, err := find("h-ops")
	if err != nil || k.ID != id || k.Scope != "operator" {
		t.Fatalf("find active = %+v, %v; want id %d, operator", k, err, id)
	}
	if _, err := find("nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown hash: err = %v, want ErrNotFound", err)
	}
	if err := s.WithTx(context.Background(), func(q Querier) error {
		return RevokeAPIKey(context.Background(), q, "ops", t0)
	}); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
	if _, err := find("h-ops"); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked key: err = %v, want ErrNotFound", err)
	}
}

// TestRevokeAPIKeyOnce checks that a revocation sets revoked_at on the active key,
// and that a second revocation of the same name is not found: the first time stands.
func TestRevokeAPIKeyOnce(t *testing.T) {
	s := openTemp(t)
	t0 := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	addKey(t, s, keyAt("ci", "h-ci", "agent", t0))
	first := t0.Add(time.Hour)
	revoke := func(at time.Time) error {
		return s.WithTx(context.Background(), func(q Querier) error {
			return RevokeAPIKey(context.Background(), q, "ci", at)
		})
	}
	if err := revoke(first); err != nil {
		t.Fatalf("first revoke: %v", err)
	}
	if err := revoke(first.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("second revoke: err = %v, want ErrNotFound", err)
	}
	var list []APIKeyInfo
	_ = s.WithReadTx(context.Background(), func(q Querier) error {
		var err error
		list, err = ListAPIKeys(context.Background(), q)
		return err
	})
	if len(list) != 1 || list[0].RevokedAt == nil || !list[0].RevokedAt.Equal(first) {
		t.Errorf("revoked_at = %+v, want %v", list, first)
	}
}

// TestTouchAPIKey checks that touch sets last_used_at, and that a touch of an id
// with no row is not found.
func TestTouchAPIKey(t *testing.T) {
	s := openTemp(t)
	t0 := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	id := addKey(t, s, keyAt("ci", "h-ci", "agent", t0))
	used := t0.Add(5 * time.Minute)
	if err := s.WithTx(context.Background(), func(q Querier) error {
		return TouchAPIKey(context.Background(), q, id, used)
	}); err != nil {
		t.Fatalf("TouchAPIKey: %v", err)
	}
	var list []APIKeyInfo
	_ = s.WithReadTx(context.Background(), func(q Querier) error {
		var err error
		list, err = ListAPIKeys(context.Background(), q)
		return err
	})
	if len(list) != 1 || list[0].LastUsedAt == nil || !list[0].LastUsedAt.Equal(used) {
		t.Errorf("last_used_at = %+v, want %v", list, used)
	}
	err := s.WithTx(context.Background(), func(q Querier) error {
		return TouchAPIKey(context.Background(), q, id+99, used)
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("touch of a missing id: err = %v, want ErrNotFound", err)
	}
}

// TestAPIKeyMigrationRefusesARow checks the assert in migration 0004. A v1 database
// that holds an api_keys row is not migrated: the migration fails, and the row is
// still there. Replacing the table must never drop a key unseen.
func TestAPIKeyMigrationRefusesARow(t *testing.T) {
	list, err := loadMigrations(migrationFiles)
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cairn.db")+"?"+dsnOptions)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrate(db, list[:3]); err != nil {
		t.Fatalf("migrate to v3: %v", err)
	}
	// The v1 shape: scopes, not scope.
	if _, err := db.Exec(`INSERT INTO api_keys (name, hash, scopes, created_at)
		VALUES ('old', 'h-old', 'agent', '2026-10-30T09:00:00.000000000Z')`); err != nil {
		t.Fatalf("insert v1 key: %v", err)
	}
	err = migrate(db, list)
	// The refusal is the CHECK of the assert, and its column names the cause. A
	// failure of any other kind, such as a syntax error, must not pass this test.
	if err == nil || !strings.Contains(err.Error(), "CHECK constraint failed: api_keys_must_be_empty") {
		t.Fatalf("migrate with a key row: err = %v, want the assert's CHECK failure from migration 0004", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE name = 'old'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("the v1 key row: count %d, err %v; want it kept", n, err)
	}
	var v int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v); err != nil || v != 3 {
		t.Fatalf("schema version = %d, err %v; want 3 after the refusal", v, err)
	}
}
