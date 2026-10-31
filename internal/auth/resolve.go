package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrUnauthenticated is what Resolve returns for a header that is missing,
// malformed, names no key, or names a revoked key. It carries no detail: the
// caller learns that the key did not pass, and nothing about which fault it was.
var ErrUnauthenticated = errors.New("unauthenticated")

// ErrUnknownKey is what a Lookup returns when no active key has the hash. The
// resolver turns it into ErrUnauthenticated.
var ErrUnknownKey = errors.New("no active key has this hash")

// Key is an active key as the resolver needs it: its id for the touch, its name
// for the operator, and its scope, which sets the role.
type Key struct {
	ID    int64
	Name  string
	Scope Scope
}

// Lookup is what the resolver needs from the key table. The store adapter in
// internal/api implements it; this package names no store type.
type Lookup interface {
	// FindActive returns the unrevoked key whose hash is hash, or an error that
	// wraps ErrUnknownKey when there is none.
	FindActive(ctx context.Context, hash string) (Key, error)
	// Touch sets last_used_at of the key with id to at.
	Touch(ctx context.Context, id int64, at time.Time) error
}

// touchEvery is the least time between two last_used_at writes for one key. A
// read must not become a write on every call, so the stamp is kept to the minute.
const touchEvery = time.Minute

// Resolver checks the Authorization header of a request and returns the key it
// names. It is safe for concurrent use.
type Resolver struct {
	lookup         Lookup
	onTouchFailure func(error)
	now            func() time.Time

	mu      sync.Mutex
	touched map[int64]time.Time // when each key's last_used_at was last written
}

// NewResolver returns a resolver over lookup. onTouchFailure receives the error of
// a last_used_at write that failed; the call that used the key still succeeds, so
// a failed stamp never refuses a read. It may be nil, which discards the error.
func NewResolver(lookup Lookup, onTouchFailure func(error)) *Resolver {
	return &Resolver{
		lookup:         lookup,
		onTouchFailure: onTouchFailure,
		now:            time.Now,
		touched:        map[int64]time.Time{},
	}
}

// Resolve returns the active key named by an Authorization header value. A header
// that is missing, malformed or names no active key returns an error that wraps
// ErrUnauthenticated. Any other error is a failure of the lookup itself, and the
// caller treats it as internal. Neither error carries the key or the header.
func (r *Resolver) Resolve(ctx context.Context, header string) (Key, error) {
	raw, ok := bearerKey(header)
	if !ok {
		return Key{}, ErrUnauthenticated
	}
	k, err := r.lookup.FindActive(ctx, HashKey(raw))
	if errors.Is(err, ErrUnknownKey) {
		return Key{}, ErrUnauthenticated
	}
	if err != nil {
		return Key{}, fmt.Errorf("auth: find key: %w", err)
	}
	if !ValidScope(k.Scope) {
		return Key{}, fmt.Errorf("auth: key %s has scope %q, which is none of the three", k.Name, k.Scope)
	}
	r.touch(ctx, k.ID)
	return k, nil
}

// touch writes last_used_at for the key with id, unless it was written less than
// touchEvery ago. The time is stamped under the lock before the write, so two
// concurrent calls for one key cannot both write. The write uses a context that a
// client disconnect does not cancel, so a stamp is not lost to it.
func (r *Resolver) touch(ctx context.Context, id int64) {
	now := r.now()
	r.mu.Lock()
	if last, seen := r.touched[id]; seen && now.Sub(last) < touchEvery {
		r.mu.Unlock()
		return
	}
	r.touched[id] = now
	r.mu.Unlock()
	if err := r.lookup.Touch(context.WithoutCancel(ctx), id, now); err != nil && r.onTouchFailure != nil {
		r.onTouchFailure(err)
	}
}
