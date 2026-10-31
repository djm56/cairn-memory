package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// KeyCaller returns the CallerFunc of a server that checks API keys (DESIGN.md
// §5.4, §8). The key in the Authorization header gives the role, by its scope:
// agent, operator or viewer. X-Cairn-Role is not read here, so a client cannot
// claim a role the key does not carry (issue #110). The project, agent, session
// and client still come from the X-Cairn-* headers, as they do for every caller.
//
// A missing, malformed, unknown or revoked key is a core.Error of kind
// KindUnauthenticated, answered 401. A failure of the lookup is returned as it
// is, and the call answers it as internal.
func KeyCaller(res *auth.Resolver) CallerFunc {
	return func(r *http.Request) (core.Caller, error) {
		key, err := res.Resolve(r.Context(), r.Header.Get("Authorization"))
		if errors.Is(err, auth.ErrUnauthenticated) {
			return core.Caller{}, &core.Error{Kind: KindUnauthenticated, Message: unauthenticatedMessage}
		}
		if err != nil {
			return core.Caller{}, err
		}
		role, err := roleOf(key.Scope)
		if err != nil {
			return core.Caller{}, err
		}
		return core.Caller{
			Project: r.Header.Get(HeaderProject),
			Agent:   r.Header.Get(HeaderAgent),
			Session: r.Header.Get(HeaderSession),
			Client:  r.Header.Get(HeaderClient),
			Role:    role,
		}, nil
	}
}

// roleOf maps a key's scope to the role a call carries. An unknown scope has no
// role, and is an error: it must not grant anything by default.
func roleOf(s auth.Scope) (core.Role, error) {
	switch s {
	case auth.ScopeAgent:
		return core.RoleAgent, nil
	case auth.ScopeOperator:
		return core.RoleOperator, nil
	case auth.ScopeViewer:
		return core.RoleViewer, nil
	}
	return "", fmt.Errorf("api: scope %q has no role", s)
}

// storeLookup adapts a *store.Store to auth.Lookup. It is the one place the key
// resolver reaches the database.
type storeLookup struct {
	st *store.Store
}

// NewStoreLookup returns the key table of st as the lookup a resolver needs.
func NewStoreLookup(st *store.Store) auth.Lookup {
	return storeLookup{st: st}
}

// FindActive returns the active key with hash. A hash with no active key is
// auth.ErrUnknownKey, so the resolver refuses it the same way as a malformed key.
func (l storeLookup) FindActive(ctx context.Context, hash string) (auth.Key, error) {
	var row store.APIKeyInfo
	err := l.st.WithReadTx(ctx, func(q store.Querier) error {
		var err error
		row, err = store.FindActiveAPIKey(ctx, q, hash)
		return err
	})
	if errors.Is(err, store.ErrNotFound) {
		return auth.Key{}, auth.ErrUnknownKey
	}
	if err != nil {
		return auth.Key{}, err
	}
	return auth.Key{ID: row.ID, Name: row.Name, Scope: auth.Scope(row.Scope)}, nil
}

// Touch sets last_used_at of the key with id to at.
func (l storeLookup) Touch(ctx context.Context, id int64, at time.Time) error {
	return l.st.WithTx(ctx, func(q store.Querier) error {
		return store.TouchAPIKey(ctx, q, id, at)
	})
}
