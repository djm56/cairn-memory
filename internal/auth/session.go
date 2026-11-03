package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// The lifetimes of a web UI session (DESIGN §5.5). Both are Bishop defaults, pending
// operator approval. The absolute lifetime counts from sign-in and does not slide.
// The idle lifetime counts from the last request, as last_seen_at records it.
const (
	// SessionAbsolute is how long a session lives from sign-in, whatever it does.
	SessionAbsolute = 12 * time.Hour
	// SessionIdle is how long a session lives after its last request.
	SessionIdle = 2 * time.Hour
	// sweepEvery is the least time between two cleanups of expired sessions, run from a
	// request. A sign-in always cleans up.
	sweepEvery = time.Hour
	// sessionTokenBytes is the entropy of a session token: 256 bits, as for a key.
	sessionTokenBytes = 32
)

// ErrNoSession is what Check returns for a token with no live session: a token that
// is missing, malformed, unknown, past either lifetime, or whose key is revoked, gone
// or no longer a web UI scope. It carries no detail, so a caller cannot tell them apart.
var ErrNoSession = errors.New("no live session")

// ErrAgentKey is what SignIn returns for an agent key. The web UI signs in operator and
// viewer keys only (DESIGN §5.5), so an agent key is refused by name.
var ErrAgentKey = errors.New("an agent key does not sign in to the web UI")

// NewSession is a session row to insert: the hash of its token, the key it signs in
// with, and its three times.
type NewSession struct {
	TokenHash  string
	KeyID      int64
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

// SessionRecord is a session row as the store reads it, with its key's current name,
// scope and revocation. The role is the scope, read each time, so the row keeps none.
type SessionRecord struct {
	TokenHash  string
	KeyID      int64
	KeyName    string
	Scope      Scope
	Revoked    bool // the key's revoked_at is set
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

// SessionStore is what the session rules need from the session table (DESIGN §5.5).
// The store adapter in internal/server implements it; this package names no store
// type. The store reads and writes. The rules here decide whether a row is still live.
type SessionStore interface {
	// CreateSession inserts a session row.
	CreateSession(ctx context.Context, s NewSession) error
	// ReadSession returns the row with tokenHash, joined with its key. It returns an
	// error that wraps ErrNoSession when there is no such row.
	ReadSession(ctx context.Context, tokenHash string) (SessionRecord, error)
	// TouchSession sets last_seen_at of the row with tokenHash to at. It returns an
	// error that wraps ErrNoSession when the row has gone.
	TouchSession(ctx context.Context, tokenHash string, at time.Time) error
	// DeleteSession deletes the row with tokenHash. A missing row is not an error.
	DeleteSession(ctx context.Context, tokenHash string) error
	// DeleteExpired deletes every row whose expires_at is at or before expiredBy, or
	// whose last_seen_at is at or before idleBefore, and returns how many it deleted.
	DeleteExpired(ctx context.Context, expiredBy, idleBefore time.Time) (int64, error)
}

// Session is a live web UI session as Check reports it: the key that signed in, which
// gives the name and the role, and the absolute end.
type Session struct {
	Key       Key
	ExpiresAt time.Time
}

// Sessions makes, checks and ends the web UI's sessions. It is safe for concurrent use.
type Sessions struct {
	keys      *Resolver
	store     SessionStore
	now       func() time.Time
	onFailure func(error)

	mu        sync.Mutex
	lastSweep time.Time // when expired sessions were last cleaned up from a request
}

// NewSessions returns the session rules over store, with the key resolver keys (which
// signs in a key and moves its last_used_at, so the UI and the API share one
// once-a-minute rule). now is the clock; nil means time.Now. onFailure receives a
// failure that does not refuse the request that caused it, such as a last_seen_at
// write or a cleanup that failed; nil discards it.
func NewSessions(store SessionStore, keys *Resolver, now func() time.Time, onFailure func(error)) *Sessions {
	if now == nil {
		now = time.Now
	}
	if onFailure == nil {
		onFailure = func(error) {}
	}
	return &Sessions{keys: keys, store: store, now: now, onFailure: onFailure}
}

// SignIn checks key and, when it is an operator or viewer key that is active, makes a
// session for it. It returns the session and the token to set in the cookie. previous
// is the token the browser presented, or "": its session is deleted once the new one
// exists, so a browser holds one session. A key that is malformed, unknown or revoked
// is ErrUnauthenticated, and an agent key is ErrAgentKey; neither makes a session, and a
// presented session is left as it was. Any other error is a failure of the store.
func (s *Sessions) SignIn(ctx context.Context, key, previous string) (Session, string, error) {
	k, err := s.keys.Find(ctx, key)
	if err != nil {
		return Session{}, "", err
	}
	if k.Scope == ScopeAgent {
		return Session{}, "", ErrAgentKey
	}
	token, err := NewToken()
	if err != nil {
		return Session{}, "", err
	}
	now := s.now()
	row := NewSession{
		TokenHash:  HashToken(token),
		KeyID:      k.ID,
		CreatedAt:  now,
		ExpiresAt:  now.Add(SessionAbsolute),
		LastSeenAt: now,
	}
	if err := s.store.CreateSession(ctx, row); err != nil {
		return Session{}, "", fmt.Errorf("auth: create session: %w", err)
	}
	if previous != "" {
		// The new session stands whether or not this delete works, and the cookie that
		// replaces the old token is sent with it. A failed delete is logged, not refused.
		if err := s.store.DeleteSession(ctx, HashToken(previous)); err != nil {
			s.onFailure(fmt.Errorf("auth: delete the previous session: %w", err))
		}
	}
	s.keys.TouchKey(ctx, k.ID)
	s.sweep(ctx, now)
	return Session{Key: k, ExpiresAt: row.ExpiresAt}, token, nil
}

// Check returns the live session of token. A session past either lifetime, whose key
// is revoked, gone or no longer an operator or viewer key, is ended at once: its row
// is deleted and the answer is ErrNoSession. A live session has last_seen_at moved to
// now at most once a minute, and its key's last_used_at moved under the same rule. The
// role comes from the key's scope now, so a key whose scope changes changes the role at
// its next request. Any other error is a failure of the store.
func (s *Sessions) Check(ctx context.Context, token string) (Session, error) {
	if !wellFormedToken(token) {
		return Session{}, ErrNoSession
	}
	hash := HashToken(token)
	now := s.now()
	rec, err := s.store.ReadSession(ctx, hash)
	if errors.Is(err, ErrNoSession) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, fmt.Errorf("auth: read session: %w", err)
	}
	if !live(rec, now) {
		s.end(ctx, hash)
		return Session{}, ErrNoSession
	}
	if now.Sub(rec.LastSeenAt) >= touchEvery {
		err := s.store.TouchSession(ctx, hash, now)
		if errors.Is(err, ErrNoSession) {
			return Session{}, ErrNoSession
		}
		if err != nil {
			// The request still runs: a failed stamp refuses nothing, but it is logged.
			s.onFailure(fmt.Errorf("auth: touch session: %w", err))
		}
	}
	s.keys.TouchKey(ctx, rec.KeyID)
	s.maybeSweep(ctx, now)
	return Session{
		Key:       Key{ID: rec.KeyID, Name: rec.KeyName, Scope: rec.Scope},
		ExpiresAt: rec.ExpiresAt,
	}, nil
}

// End deletes the session of token, for sign-out. A token with no session, or a
// malformed one, deletes nothing and is not an error. A failed delete is returned, so
// the caller can say the sign-out did not finish.
func (s *Sessions) End(ctx context.Context, token string) error {
	if !wellFormedToken(token) {
		return nil
	}
	return s.store.DeleteSession(ctx, HashToken(token))
}

// live reports whether rec is a session that may answer a request at now: before its
// absolute end, before its idle end, with a key that is not revoked and holds a web UI
// scope. A key whose scope is not operator or viewer is refused here, not by the store.
func live(rec SessionRecord, now time.Time) bool {
	if rec.Revoked {
		return false
	}
	if rec.Scope != ScopeOperator && rec.Scope != ScopeViewer {
		return false
	}
	return now.Before(rec.ExpiresAt) && now.Before(rec.LastSeenAt.Add(SessionIdle))
}

// end deletes the session with hash. A failure is logged: the caller has already
// decided the session is over, so the answer does not change.
func (s *Sessions) end(ctx context.Context, hash string) {
	if err := s.store.DeleteSession(ctx, hash); err != nil {
		s.onFailure(fmt.Errorf("auth: delete an ended session: %w", err))
	}
}

// maybeSweep cleans up expired sessions when the last cleanup was an hour or more ago,
// or never. It runs from a request, so no timer goroutine is needed.
func (s *Sessions) maybeSweep(ctx context.Context, now time.Time) {
	s.mu.Lock()
	due := s.lastSweep.IsZero() || !now.Before(s.lastSweep.Add(sweepEvery))
	s.mu.Unlock()
	if due {
		s.sweep(ctx, now)
	}
}

// sweep deletes every expired session and records the time, so the hourly check does
// not run again until an hour has passed. A failure is logged and changes nothing else.
func (s *Sessions) sweep(ctx context.Context, now time.Time) {
	s.mu.Lock()
	s.lastSweep = now
	s.mu.Unlock()
	if _, err := s.store.DeleteExpired(ctx, now, now.Add(-SessionIdle)); err != nil {
		s.onFailure(fmt.Errorf("auth: clean up expired sessions: %w", err))
	}
}

// NewToken returns a new session token: 32 bytes from crypto/rand, in unpadded
// base64url. The cookie carries it, and the store keeps only HashToken of it.
func NewToken() (string, error) {
	var b [sessionTokenBytes]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", fmt.Errorf("auth: read entropy for a session: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// HashToken returns the SHA-256 of token, in lower-case hex. It is what ui_sessions
// stores as token_hash. As with HashKey, a fast hash is enough for 256 random bits.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// wellFormedToken reports whether token has the shape NewToken gives: 32 bytes in
// unpadded base64url. It is checked before any lookup, so a cookie of any other shape
// is refused without a database read.
func wellFormedToken(token string) bool {
	b, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(b) == sessionTokenBytes && base64.RawURLEncoding.EncodeToString(b) == token
}
