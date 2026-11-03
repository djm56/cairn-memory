package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// UISessionNew is a web UI session row to insert: the hash of its token, the key
// it signs in with, and its three times. The token itself never reaches this layer.
type UISessionNew struct {
	TokenHash  string
	KeyID      int64
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

// UISessionInfo is a web UI session row joined with its key's name, scope and
// revocation, as the session check needs them (DESIGN §5.5). The role is the
// key's scope now, so it is read here, not stored on the session.
type UISessionInfo struct {
	TokenHash    string
	KeyID        int64
	KeyName      string
	KeyScope     string
	KeyRevokedAt *time.Time
	CreatedAt    time.Time
	ExpiresAt    time.Time
	LastSeenAt   time.Time
}

// CreateUISession inserts a session row. A token hash already taken is a
// constraint error from the schema, which the caller sees wrapped.
//
// Write function: call it through WithTx.
func CreateUISession(ctx context.Context, q Querier, s UISessionNew) error {
	_, err := q.ExecContext(ctx, `INSERT INTO ui_sessions
		(token_hash, key_id, created_at, expires_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)`,
		s.TokenHash, s.KeyID, formatTime(s.CreatedAt), formatTime(s.ExpiresAt), formatTime(s.LastSeenAt))
	if err != nil {
		return fmt.Errorf("store: create ui session: %w", err)
	}
	return nil
}

// ReadUISession returns the session row with token hash hash, joined with its key.
// It returns ErrNotFound (wrapped) when no row has that hash. The key join is an
// inner join, which is safe: ui_sessions.key_id references api_keys, and a key row
// cannot be deleted while a session names it.
//
// Read function: call it on s, outside a transaction.
func ReadUISession(ctx context.Context, q Querier, hash string) (UISessionInfo, error) {
	row := q.QueryRowContext(ctx, `SELECT s.token_hash, s.key_id, k.name, k.scope, k.revoked_at,
			s.created_at, s.expires_at, s.last_seen_at
		FROM ui_sessions s JOIN api_keys k ON k.id = s.key_id
		WHERE s.token_hash = ?`, hash)
	s, err := scanUISession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return s, fmt.Errorf("store: ui session: %w", ErrNotFound)
	}
	return s, err
}

// TouchUISession sets last_seen_at of the session with token hash hash. It
// returns ErrNotFound (wrapped) when the row has gone.
//
// Write function: call it through WithTx.
func TouchUISession(ctx context.Context, q Querier, hash string, at time.Time) error {
	res, err := q.ExecContext(ctx, `UPDATE ui_sessions SET last_seen_at = ? WHERE token_hash = ?`,
		formatTime(at), hash)
	if err != nil {
		return fmt.Errorf("store: touch ui session: %w", err)
	}
	return requireOneRow(res, "ui session")
}

// DeleteUISession deletes the session with token hash hash. A hash with no row
// deletes nothing and is not an error: signing out twice is the same as once.
//
// Write function: call it through WithTx.
func DeleteUISession(ctx context.Context, q Querier, hash string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM ui_sessions WHERE token_hash = ?`, hash); err != nil {
		return fmt.Errorf("store: delete ui session: %w", err)
	}
	return nil
}

// DeleteExpiredUISessions deletes every session past either lifetime, and returns
// how many went. A row is past its absolute lifetime when expires_at is at or
// before expiredBy, and past its idle lifetime when last_seen_at is at or before
// idleBefore. The caller computes both cutoffs, because the lifetimes are auth's
// (DESIGN §5.5), and the store holds no policy.
//
// Write function: call it through WithTx.
func DeleteExpiredUISessions(ctx context.Context, q Querier, expiredBy, idleBefore time.Time) (int64, error) {
	res, err := q.ExecContext(ctx, `DELETE FROM ui_sessions WHERE expires_at <= ? OR last_seen_at <= ?`,
		formatTime(expiredBy), formatTime(idleBefore))
	if err != nil {
		return 0, fmt.Errorf("store: delete expired ui sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: rows affected: %w", err)
	}
	return n, nil
}

// scanUISession reads one row of ReadUISession, in its column order. It returns the
// scan error unwrapped, so a caller can test for sql.ErrNoRows.
func scanUISession(s rowScanner) (UISessionInfo, error) {
	var u UISessionInfo
	var revoked sql.NullString
	var created, expires, seen string
	if err := s.Scan(&u.TokenHash, &u.KeyID, &u.KeyName, &u.KeyScope, &revoked,
		&created, &expires, &seen); err != nil {
		return u, err
	}
	var err error
	if u.KeyRevokedAt, err = nullTime(revoked); err != nil {
		return u, fmt.Errorf("store: ui session key revoked_at: %w", err)
	}
	if u.CreatedAt, err = parseTime(created); err != nil {
		return u, fmt.Errorf("store: ui session created_at: %w", err)
	}
	if u.ExpiresAt, err = parseTime(expires); err != nil {
		return u, fmt.Errorf("store: ui session expires_at: %w", err)
	}
	if u.LastSeenAt, err = parseTime(seen); err != nil {
		return u, fmt.Errorf("store: ui session last_seen_at: %w", err)
	}
	return u, nil
}
