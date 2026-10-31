package server

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/store"
)

// KeyInfo is one API key as cairn key list prints it. It carries no hash and no
// plaintext: the hash is only for the lookup on a call, and the plaintext exists only
// in the output of AddKey (DESIGN §8).
type KeyInfo struct {
	Name       string
	Scope      auth.Scope
	CreatedAt  time.Time
	LastUsedAt *time.Time // nil when the key has never been used
	RevokedAt  *time.Time // nil while the key is active
}

// keyNamePattern is the shape of a key name: a letter or digit, then up to 63 letters,
// digits, dots, hyphens or underscores. A name is an operator's label, so it stays short
// and printable.
var keyNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// AddKey makes a new API key named name with scope, in the database at db, and returns
// its plaintext. The database is created when it is absent. Only the hash is stored, so
// the plaintext is returned once and cannot be read back. Nothing here logs it.
//
// A name is refused when it is malformed, when it begins with the key prefix (a key
// pasted in the name position), or when a key of that name exists, active or revoked:
// the store keeps a revoked row, so its name stays taken.
func AddKey(ctx context.Context, db, name string, scope auth.Scope) (string, error) {
	if err := checkKeyName(name); err != nil {
		return "", err
	}
	if !auth.ValidScope(scope) {
		return "", fmt.Errorf("the scope %q is none of agent, operator or viewer", scope)
	}
	key, err := auth.NewKey()
	if err != nil {
		return "", err
	}
	st, err := openDatabase(db, true)
	if err != nil {
		return "", err
	}
	defer st.Close()
	err = st.WithTx(ctx, func(q store.Querier) error {
		existing, err := store.ListAPIKeys(ctx, q)
		if err != nil {
			return err
		}
		for _, k := range existing {
			if k.Name == name {
				return fmt.Errorf("a key named %s already exists, active or revoked: a name is not reused", name)
			}
		}
		_, err = store.AddAPIKey(ctx, q, store.APIKeyNew{
			Name: name, Hash: auth.HashKey(key), Scope: string(scope), CreatedAt: time.Now().UTC(),
		})
		return err
	})
	if err != nil {
		return "", fmt.Errorf("add the key: %w", err)
	}
	return key, nil
}

// checkKeyName refuses a name that is not a key name: one of the shape keyNamePattern gives,
// or one that begins with the key prefix, which is a key pasted into the name position. The
// message gives the rule and never the name, so a key given as a name is not printed back
// (step 8 review W6). AddKey and RevokeKey both check the name here, before any database
// lookup, so the two cannot disagree about what a name is.
func checkKeyName(name string) error {
	if !keyNamePattern.MatchString(name) || strings.HasPrefix(name, auth.KeyPrefix) {
		return errors.New("the key name must be 1 to 64 letters, digits, dots, hyphens or underscores, starting with a letter or digit, and must not begin with cairn_")
	}
	return nil
}

// ListKeys returns every key of the database at db, revoked ones included, in the order
// they were made. The database must exist.
func ListKeys(ctx context.Context, db string) ([]KeyInfo, error) {
	st, err := openDatabase(db, false)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	var rows []store.APIKeyInfo
	err = st.WithReadTx(ctx, func(q store.Querier) error {
		var err error
		rows, err = store.ListAPIKeys(ctx, q)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list the keys: %w", err)
	}
	out := make([]KeyInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, KeyInfo{
			Name:       r.Name,
			Scope:      auth.Scope(r.Scope),
			CreatedAt:  r.CreatedAt,
			LastUsedAt: r.LastUsedAt,
			RevokedAt:  r.RevokedAt,
		})
	}
	return out, nil
}

// RevokeKey revokes the active key named name in the database at db. The first
// revocation stands; a second one, or a name with no key, is an error that says so. The
// database must exist. A running server reads the key table on every call, so the key
// stops working at once. The name is checked as AddKey checks it, before the database is
// opened, so a name that is not a key name, a key pasted into it for one, is refused with
// the rule and is never echoed.
func RevokeKey(ctx context.Context, db, name string) error {
	if err := checkKeyName(name); err != nil {
		return err
	}
	st, err := openDatabase(db, false)
	if err != nil {
		return err
	}
	defer st.Close()
	err = st.WithTx(ctx, func(q store.Querier) error {
		return store.RevokeAPIKey(ctx, q, name, time.Now().UTC())
	})
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no active API key named %s: it was never made, or it is already revoked", name)
	}
	if err != nil {
		return fmt.Errorf("revoke the key: %w", err)
	}
	return nil
}
