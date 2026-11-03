package server

import (
	"context"
	"errors"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/store"
)

// sessionStore adapts a *store.Store to auth.SessionStore, as storeLookup adapts it to
// auth.Lookup. It is the one place the web UI's session rows reach the database. It
// writes and reads; the session rules are in auth.
type sessionStore struct {
	st *store.Store
}

// NewSessionStore returns the ui_sessions table of st as the store a session service
// needs.
func NewSessionStore(st *store.Store) auth.SessionStore {
	return sessionStore{st: st}
}

// CreateSession inserts the session row.
func (s sessionStore) CreateSession(ctx context.Context, n auth.NewSession) error {
	return s.st.WithTx(ctx, func(q store.Querier) error {
		return store.CreateUISession(ctx, q, store.UISessionNew{
			TokenHash:  n.TokenHash,
			KeyID:      n.KeyID,
			CreatedAt:  n.CreatedAt,
			ExpiresAt:  n.ExpiresAt,
			LastSeenAt: n.LastSeenAt,
		})
	})
}

// ReadSession returns the row with tokenHash and its key's state. A row that does not
// exist is auth.ErrNoSession.
func (s sessionStore) ReadSession(ctx context.Context, tokenHash string) (auth.SessionRecord, error) {
	var row store.UISessionInfo
	err := s.st.WithReadTx(ctx, func(q store.Querier) error {
		var err error
		row, err = store.ReadUISession(ctx, q, tokenHash)
		return err
	})
	if errors.Is(err, store.ErrNotFound) {
		return auth.SessionRecord{}, auth.ErrNoSession
	}
	if err != nil {
		return auth.SessionRecord{}, err
	}
	return auth.SessionRecord{
		TokenHash:  row.TokenHash,
		KeyID:      row.KeyID,
		KeyName:    row.KeyName,
		Scope:      auth.Scope(row.KeyScope),
		Revoked:    row.KeyRevokedAt != nil,
		CreatedAt:  row.CreatedAt,
		ExpiresAt:  row.ExpiresAt,
		LastSeenAt: row.LastSeenAt,
	}, nil
}

// TouchSession sets last_seen_at of the row. A row that has gone is auth.ErrNoSession.
func (s sessionStore) TouchSession(ctx context.Context, tokenHash string, at time.Time) error {
	err := s.st.WithTx(ctx, func(q store.Querier) error {
		return store.TouchUISession(ctx, q, tokenHash, at)
	})
	if errors.Is(err, store.ErrNotFound) {
		return auth.ErrNoSession
	}
	return err
}

// DeleteSession deletes the row with tokenHash. A missing row is not an error.
func (s sessionStore) DeleteSession(ctx context.Context, tokenHash string) error {
	return s.st.WithTx(ctx, func(q store.Querier) error {
		return store.DeleteUISession(ctx, q, tokenHash)
	})
}

// DeleteExpired deletes the rows past either cutoff, and returns their count.
func (s sessionStore) DeleteExpired(ctx context.Context, expiredBy, idleBefore time.Time) (int64, error) {
	var n int64
	err := s.st.WithTx(ctx, func(q store.Querier) error {
		var err error
		n, err = store.DeleteExpiredUISessions(ctx, q, expiredBy, idleBefore)
		return err
	})
	return n, err
}
