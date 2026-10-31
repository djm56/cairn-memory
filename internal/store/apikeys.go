package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AddAPIKey writes a new key row and returns its id. A name or a hash that is
// already taken is a constraint error from the schema, which the caller sees
// wrapped. The hash is written as given; the plaintext never reaches this layer.
//
// Write function: call it through WithTx.
func AddAPIKey(ctx context.Context, q Querier, k APIKeyNew) (int64, error) {
	res, err := q.ExecContext(ctx, `INSERT INTO api_keys
		(name, hash, scope, created_at)
		VALUES (?, ?, ?, ?)`,
		k.Name, k.Hash, k.Scope, formatTime(k.CreatedAt))
	if err != nil {
		return 0, fmt.Errorf("store: add api key %s: %w", k.Name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: add api key %s: last id: %w", k.Name, err)
	}
	return id, nil
}

// ListAPIKeys returns every key, revoked ones included, in id order. The hash is
// not in the result.
//
// Read function: call it on s, outside a transaction.
func ListAPIKeys(ctx context.Context, q Querier) ([]APIKeyInfo, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, name, scope, created_at, last_used_at, revoked_at
		FROM api_keys ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("store: list api keys: %w", err)
	}
	defer rows.Close()
	var out []APIKeyInfo
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list api keys: %w", err)
	}
	return out, nil
}

// RevokeAPIKey sets revoked_at on the active key named name. A key with no such
// name, or one already revoked, returns ErrNotFound (wrapped): the first
// revocation stands, and a second one changes nothing. The row is kept, so the
// name stays taken.
//
// Write function: call it through WithTx.
func RevokeAPIKey(ctx context.Context, q Querier, name string, at time.Time) error {
	res, err := q.ExecContext(ctx, `UPDATE api_keys SET revoked_at = ?
		WHERE name = ? AND revoked_at IS NULL`, formatTime(at), name)
	if err != nil {
		return fmt.Errorf("store: revoke api key %s: %w", name, err)
	}
	return requireOneRow(res, fmt.Sprintf("active api key %s", name))
}

// FindActiveAPIKey returns the unrevoked key whose hash is hash. It returns
// ErrNotFound (wrapped) when no active key has that hash, which covers a revoked
// key and an unknown one alike.
//
// Read function: call it on s, outside a transaction.
func FindActiveAPIKey(ctx context.Context, q Querier, hash string) (APIKeyInfo, error) {
	row := q.QueryRowContext(ctx, `SELECT id, name, scope, created_at, last_used_at, revoked_at
		FROM api_keys WHERE hash = ? AND revoked_at IS NULL`, hash)
	k, err := scanAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return k, fmt.Errorf("store: active api key: %w", ErrNotFound)
	}
	return k, err
}

// TouchAPIKey sets last_used_at on the key with id. It returns ErrNotFound
// (wrapped) when no row has that id.
//
// Write function: call it through WithTx.
func TouchAPIKey(ctx context.Context, q Querier, id int64, at time.Time) error {
	res, err := q.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
		formatTime(at), id)
	if err != nil {
		return fmt.Errorf("store: touch api key %d: %w", id, err)
	}
	return requireOneRow(res, fmt.Sprintf("api key %d", id))
}

// scanAPIKey reads one api_keys row, in the column order the queries above
// select. It returns the error of the scan unwrapped, so a caller can test for
// sql.ErrNoRows.
func scanAPIKey(s rowScanner) (APIKeyInfo, error) {
	var k APIKeyInfo
	var created string
	var lastUsed, revoked sql.NullString
	if err := s.Scan(&k.ID, &k.Name, &k.Scope, &created, &lastUsed, &revoked); err != nil {
		return k, err
	}
	var err error
	if k.CreatedAt, err = parseTime(created); err != nil {
		return k, fmt.Errorf("store: api key %d created_at: %w", k.ID, err)
	}
	if k.LastUsedAt, err = nullTime(lastUsed); err != nil {
		return k, fmt.Errorf("store: api key %d last_used_at: %w", k.ID, err)
	}
	if k.RevokedAt, err = nullTime(revoked); err != nil {
		return k, fmt.Errorf("store: api key %d revoked_at: %w", k.ID, err)
	}
	return k, nil
}
