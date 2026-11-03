package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// uiSession is a session row for the tests: a hash, the key it names, and its times,
// made at t0 with an absolute end at t0+12h and a last_seen_at of t0.
func uiSession(hash string, keyID int64, t0 time.Time) UISessionNew {
	return UISessionNew{
		TokenHash:  hash,
		KeyID:      keyID,
		CreatedAt:  t0,
		ExpiresAt:  t0.Add(12 * time.Hour),
		LastSeenAt: t0,
	}
}

// createSession inserts s in its own write transaction.
func createSession(t *testing.T, st *Store, s UISessionNew) {
	t.Helper()
	if err := st.WithTx(context.Background(), func(q Querier) error {
		return CreateUISession(context.Background(), q, s)
	}); err != nil {
		t.Fatalf("CreateUISession %s: %v", s.TokenHash, err)
	}
}

// sessionHashes returns the token hashes still in ui_sessions, in hash order.
func sessionHashes(t *testing.T, st *Store) []string {
	t.Helper()
	rows, err := st.db.Query(`SELECT token_hash FROM ui_sessions ORDER BY token_hash`)
	if err != nil {
		t.Fatalf("read ui_sessions: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			t.Fatal(err)
		}
		out = append(out, h)
	}
	return out
}

// TestMigrationSixCreatesTheUITables checks that a fresh database has both tables of
// migration 0006, with the two indexes of ui_sessions, and no reaffirmation rows yet.
func TestMigrationSixCreatesTheUITables(t *testing.T) {
	s := openTemp(t)
	for _, name := range []string{"ui_sessions", "directive_reaffirmations"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, name).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s: count %d (%v), want 1", name, n, err)
		}
	}
	for _, name := range []string{"ui_sessions_expires_at", "ui_sessions_key_id"} {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&n); err != nil || n != 1 {
			t.Errorf("index %s: count %d (%v), want 1", name, n, err)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM directive_reaffirmations`).Scan(&n); err != nil || n != 0 {
		t.Errorf("directive_reaffirmations rows = %d (%v), want 0", n, err)
	}
}

// TestUISessionCreateAndReadJoinsTheKey checks that a session reads back with its
// key's name, scope and revocation, and with its three times in the stored form.
func TestUISessionCreateAndReadJoinsTheKey(t *testing.T) {
	st := openTemp(t)
	t0 := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	keyID := addKey(t, st, keyAt("ops", "hash-ops", "operator", t0))
	createSession(t, st, uiSession("tok-1", keyID, t0))

	var got UISessionInfo
	if err := st.WithReadTx(context.Background(), func(q Querier) error {
		var err error
		got, err = ReadUISession(context.Background(), q, "tok-1")
		return err
	}); err != nil {
		t.Fatalf("ReadUISession: %v", err)
	}
	if got.KeyID != keyID || got.KeyName != "ops" || got.KeyScope != "operator" || got.KeyRevokedAt != nil {
		t.Errorf("joined key = id %d name %q scope %q revoked %v; want %d ops operator nil",
			got.KeyID, got.KeyName, got.KeyScope, got.KeyRevokedAt, keyID)
	}
	if !got.CreatedAt.Equal(t0) || !got.ExpiresAt.Equal(t0.Add(12*time.Hour)) || !got.LastSeenAt.Equal(t0) {
		t.Errorf("times = %v %v %v; want %v, +12h, %v", got.CreatedAt, got.ExpiresAt, got.LastSeenAt, t0, t0)
	}
}

// TestUISessionReadShowsARevokedKey checks that the join carries a key's revocation,
// so auth can end the session at its next request.
func TestUISessionReadShowsARevokedKey(t *testing.T) {
	st := openTemp(t)
	t0 := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	keyID := addKey(t, st, keyAt("viewer-1", "hash-v", "viewer", t0))
	createSession(t, st, uiSession("tok-r", keyID, t0))
	if err := st.WithTx(context.Background(), func(q Querier) error {
		return RevokeAPIKey(context.Background(), q, "viewer-1", t0.Add(time.Minute))
	}); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
	var got UISessionInfo
	if err := st.WithReadTx(context.Background(), func(q Querier) error {
		var err error
		got, err = ReadUISession(context.Background(), q, "tok-r")
		return err
	}); err != nil {
		t.Fatalf("ReadUISession: %v", err)
	}
	if got.KeyRevokedAt == nil || !got.KeyRevokedAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("KeyRevokedAt = %v; want %v", got.KeyRevokedAt, t0.Add(time.Minute))
	}
}

// TestUISessionReadNotFound checks that an unknown hash is ErrNotFound.
func TestUISessionReadNotFound(t *testing.T) {
	st := openTemp(t)
	err := st.WithReadTx(context.Background(), func(q Querier) error {
		_, err := ReadUISession(context.Background(), q, "no-such-hash")
		return err
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ReadUISession of an unknown hash = %v, want ErrNotFound", err)
	}
}

// TestUISessionRefusesAnUnknownKey checks the foreign key: a session names a key row
// that must exist.
func TestUISessionRefusesAnUnknownKey(t *testing.T) {
	st := openTemp(t)
	t0 := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	err := st.WithTx(context.Background(), func(q Querier) error {
		return CreateUISession(context.Background(), q, uiSession("tok-x", 999, t0))
	})
	if err == nil {
		t.Fatal("CreateUISession with no such key was accepted; want a foreign key error")
	}
}

// TestUISessionTouchMovesLastSeen checks that TouchUISession sets last_seen_at, and
// that a touch of a missing row is ErrNotFound.
func TestUISessionTouchMovesLastSeen(t *testing.T) {
	st := openTemp(t)
	t0 := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	keyID := addKey(t, st, keyAt("ops", "hash-ops", "operator", t0))
	createSession(t, st, uiSession("tok-t", keyID, t0))
	later := t0.Add(5 * time.Minute)
	if err := st.WithTx(context.Background(), func(q Querier) error {
		return TouchUISession(context.Background(), q, "tok-t", later)
	}); err != nil {
		t.Fatalf("TouchUISession: %v", err)
	}
	var got UISessionInfo
	if err := st.WithReadTx(context.Background(), func(q Querier) error {
		var err error
		got, err = ReadUISession(context.Background(), q, "tok-t")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !got.LastSeenAt.Equal(later) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, later)
	}
	err := st.WithTx(context.Background(), func(q Querier) error {
		return TouchUISession(context.Background(), q, "no-such-hash", later)
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("TouchUISession of a missing row = %v, want ErrNotFound", err)
	}
}

// TestUISessionDeleteIsIdempotent checks that DeleteUISession removes the row, and
// that deleting it again, or a hash that never existed, is not an error.
func TestUISessionDeleteIsIdempotent(t *testing.T) {
	st := openTemp(t)
	t0 := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	keyID := addKey(t, st, keyAt("ops", "hash-ops", "operator", t0))
	createSession(t, st, uiSession("tok-d", keyID, t0))
	for _, hash := range []string{"tok-d", "tok-d", "never-existed"} {
		if err := st.WithTx(context.Background(), func(q Querier) error {
			return DeleteUISession(context.Background(), q, hash)
		}); err != nil {
			t.Fatalf("DeleteUISession %s: %v", hash, err)
		}
	}
	if got := sessionHashes(t, st); len(got) != 0 {
		t.Errorf("sessions after delete = %v, want none", got)
	}
}

// TestDeleteExpiredUISessionsUsesBothCutoffs checks each lifetime on its own. A row
// past its absolute end goes whatever its last_seen_at; a row past its idle end goes
// whatever its expires_at; a live row stays, and the count is the rows deleted.
func TestDeleteExpiredUISessionsUsesBothCutoffs(t *testing.T) {
	st := openTemp(t)
	t0 := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	keyID := addKey(t, st, keyAt("ops", "hash-ops", "operator", t0))
	now := t0.Add(13 * time.Hour)

	// absolute: expires at t0+12h, so past its end now, though it was seen just now.
	absolute := uiSession("abs", keyID, t0)
	absolute.LastSeenAt = now.Add(-time.Minute)
	createSession(t, st, absolute)
	// idle: last seen at t0, so past 2h idle now, though its absolute end is later.
	idle := UISessionNew{TokenHash: "idle", KeyID: keyID, CreatedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(11 * time.Hour), LastSeenAt: t0}
	createSession(t, st, idle)
	// live: expires at now+1h, seen a minute ago.
	live := UISessionNew{TokenHash: "live", KeyID: keyID, CreatedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(time.Hour), LastSeenAt: now.Add(-time.Minute)}
	createSession(t, st, live)

	var n int64
	if err := st.WithTx(context.Background(), func(q Querier) error {
		var err error
		n, err = DeleteExpiredUISessions(context.Background(), q, now, now.Add(-2*time.Hour))
		return err
	}); err != nil {
		t.Fatalf("DeleteExpiredUISessions: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d, want 2", n)
	}
	if got := strings.Join(sessionHashes(t, st), ","); got != "live" {
		t.Errorf("remaining sessions = %q, want live", got)
	}
}

// TestDeleteExpiredUISessionsKeepsARowAtTheBoundary checks the boundary: a row whose
// absolute end is exactly now is past it, and a row seen exactly 2 hours ago is past
// its idle end. Both go. A row one nanosecond short of either stays.
func TestDeleteExpiredUISessionsKeepsARowAtTheBoundary(t *testing.T) {
	st := openTemp(t)
	t0 := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	keyID := addKey(t, st, keyAt("ops", "hash-ops", "operator", t0))
	now := t0.Add(6 * time.Hour)
	createSession(t, st, UISessionNew{TokenHash: "at-end", KeyID: keyID, CreatedAt: t0,
		ExpiresAt: now, LastSeenAt: now})
	createSession(t, st, UISessionNew{TokenHash: "just-short", KeyID: keyID, CreatedAt: t0,
		ExpiresAt: now.Add(time.Nanosecond), LastSeenAt: now.Add(-2*time.Hour + time.Nanosecond)})
	createSession(t, st, UISessionNew{TokenHash: "idle-edge", KeyID: keyID, CreatedAt: t0,
		ExpiresAt: now.Add(time.Hour), LastSeenAt: now.Add(-2 * time.Hour)})
	if err := st.WithTx(context.Background(), func(q Querier) error {
		_, err := DeleteExpiredUISessions(context.Background(), q, now, now.Add(-2*time.Hour))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sessionHashes(t, st), ","); got != "just-short" {
		t.Errorf("remaining = %q, want just-short only (at-end and idle-edge are past their ends)", got)
	}
}
