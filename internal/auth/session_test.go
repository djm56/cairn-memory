package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeKey is a key row of the fake database.
type fakeKey struct {
	id      int64
	name    string
	scope   Scope
	hash    string
	revoked bool
}

// fakeDB is an in-memory key and session table. It is both the Lookup of the resolver
// and the SessionStore, and it follows the store's rules: a session is deleted by
// DeleteExpired on either cutoff, and a session read joins its key.
type fakeDB struct {
	keys      map[int64]*fakeKey
	sessions  map[string]NewSession
	keyTouch  int
	seenTouch []time.Time // the at of each TouchSession, in order
	touchErr  error       // returned by TouchSession when set
	sweeps    int         // calls to DeleteExpired
	deleteErr error       // returned by DeleteSession when set
}

// newFakeDB returns an empty fake database.
func newFakeDB() *fakeDB {
	return &fakeDB{keys: map[int64]*fakeKey{}, sessions: map[string]NewSession{}}
}

// addKey adds an active key named name with scope, and returns its plaintext and id.
func (d *fakeDB) addKey(t *testing.T, name string, scope Scope) (string, int64) {
	t.Helper()
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	id := int64(len(d.keys) + 1)
	d.keys[id] = &fakeKey{id: id, name: name, scope: scope, hash: HashKey(key)}
	return key, id
}

// FindActive implements Lookup over the key table.
func (d *fakeDB) FindActive(_ context.Context, hash string) (Key, error) {
	for _, k := range d.keys {
		if k.hash == hash && !k.revoked {
			return Key{ID: k.id, Name: k.name, Scope: k.scope}, nil
		}
	}
	return Key{}, fmt.Errorf("fake: %w", ErrUnknownKey)
}

// Touch implements Lookup: it counts the key touches.
func (d *fakeDB) Touch(_ context.Context, _ int64, _ time.Time) error {
	d.keyTouch++
	return nil
}

// CreateSession implements SessionStore.
func (d *fakeDB) CreateSession(_ context.Context, s NewSession) error {
	if _, dup := d.sessions[s.TokenHash]; dup {
		return errors.New("fake: duplicate token hash")
	}
	d.sessions[s.TokenHash] = s
	return nil
}

// ReadSession implements SessionStore: the row with its key's current state.
func (d *fakeDB) ReadSession(_ context.Context, hash string) (SessionRecord, error) {
	s, ok := d.sessions[hash]
	if !ok {
		return SessionRecord{}, fmt.Errorf("fake: %w", ErrNoSession)
	}
	k := d.keys[s.KeyID]
	return SessionRecord{
		TokenHash: hash, KeyID: k.id, KeyName: k.name, Scope: k.scope, Revoked: k.revoked,
		CreatedAt: s.CreatedAt, ExpiresAt: s.ExpiresAt, LastSeenAt: s.LastSeenAt,
	}, nil
}

// TouchSession implements SessionStore: it records the time and moves last_seen_at.
func (d *fakeDB) TouchSession(_ context.Context, hash string, at time.Time) error {
	d.seenTouch = append(d.seenTouch, at)
	if d.touchErr != nil {
		return d.touchErr
	}
	s, ok := d.sessions[hash]
	if !ok {
		return fmt.Errorf("fake: %w", ErrNoSession)
	}
	s.LastSeenAt = at
	d.sessions[hash] = s
	return nil
}

// DeleteSession implements SessionStore.
func (d *fakeDB) DeleteSession(_ context.Context, hash string) error {
	if d.deleteErr != nil {
		return d.deleteErr
	}
	delete(d.sessions, hash)
	return nil
}

// DeleteExpired implements SessionStore, with the cutoffs of the store's rule.
func (d *fakeDB) DeleteExpired(_ context.Context, expiredBy, idleBefore time.Time) (int64, error) {
	d.sweeps++
	var n int64
	for hash, s := range d.sessions {
		if !s.ExpiresAt.After(expiredBy) || !s.LastSeenAt.After(idleBefore) {
			delete(d.sessions, hash)
			n++
		}
	}
	return n, nil
}

// clock is the injected time of a test.
type clock struct{ t time.Time }

// now returns the clock's time; it is the now function of NewSessions.
func (c *clock) now() time.Time { return c.t }

// failures collects the failures NewSessions reports.
type failures struct{ got []error }

// record is the onFailure function of NewSessions.
func (f *failures) record(err error) { f.got = append(f.got, err) }

// sessionFixture is a sessions service over a fake database and a clock that starts
// at t0, with an operator key and a viewer key already made.
type sessionFixture struct {
	db       *fakeDB
	clock    *clock
	failures *failures
	sessions *Sessions
	opKey    string
	opID     int64
	viewKey  string
	viewID   int64
}

// newSessionFixture builds the fixture. t0 is the time of the first call.
func newSessionFixture(t *testing.T) *sessionFixture {
	t.Helper()
	db := newFakeDB()
	op, opID := db.addKey(t, "ops", ScopeOperator)
	view, viewID := db.addKey(t, "watch", ScopeViewer)
	c := &clock{t: time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)}
	f := &failures{}
	resolver := NewResolver(db, nil)
	return &sessionFixture{
		db: db, clock: c, failures: f,
		sessions: NewSessions(db, resolver, c.now, f.record),
		opKey:    op, opID: opID, viewKey: view, viewID: viewID,
	}
}

// signIn signs in with key and fails the test on an error. It returns the token.
func (fx *sessionFixture) signIn(t *testing.T, key, previous string) string {
	t.Helper()
	_, token, err := fx.sessions.SignIn(context.Background(), key, previous)
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	return token
}

// TestSignInMakesASessionAndStoresOnlyItsHash checks that the token is 32 random bytes
// in base64url, that the store holds its hash and not the token, and that the token
// checks back to the operator's key.
func TestSignInMakesASessionAndStoresOnlyItsHash(t *testing.T) {
	fx := newSessionFixture(t)
	sess, token, err := fx.sessions.SignIn(context.Background(), fx.opKey, "")
	if err != nil {
		t.Fatalf("SignIn: %v", err)
	}
	if !wellFormedToken(token) {
		t.Errorf("token %q is not 32 bytes of base64url", token)
	}
	if _, stored := fx.db.sessions[token]; stored {
		t.Error("the raw token is a store key: the store must keep only its hash")
	}
	row, ok := fx.db.sessions[HashToken(token)]
	if !ok {
		t.Fatal("no session row under the hash of the token")
	}
	if !row.ExpiresAt.Equal(fx.clock.t.Add(SessionAbsolute)) {
		t.Errorf("ExpiresAt = %v, want sign-in + %v", row.ExpiresAt, SessionAbsolute)
	}
	if sess.Key.Name != "ops" || sess.Key.Scope != ScopeOperator {
		t.Errorf("session key = %+v, want ops operator", sess.Key)
	}
	got, err := fx.sessions.Check(context.Background(), token)
	if err != nil || got.Key.Name != "ops" {
		t.Errorf("Check of a fresh token = %+v, %v; want the ops key", got, err)
	}
}

// TestSignInAcceptsAViewerAndRefusesAnAgent checks the scope rule of sign-in: a viewer
// signs in, an agent is refused by name, and the refused one makes no row.
func TestSignInAcceptsAViewerAndRefusesAnAgent(t *testing.T) {
	fx := newSessionFixture(t)
	agent, _ := fx.db.addKey(t, "claude", ScopeAgent)
	if _, _, err := fx.sessions.SignIn(context.Background(), agent, ""); !errors.Is(err, ErrAgentKey) {
		t.Fatalf("SignIn with an agent key = %v, want ErrAgentKey", err)
	}
	if len(fx.db.sessions) != 0 {
		t.Errorf("a refused agent made %d session rows", len(fx.db.sessions))
	}
	sess, _, err := fx.sessions.SignIn(context.Background(), fx.viewKey, "")
	if err != nil || sess.Key.Scope != ScopeViewer {
		t.Errorf("SignIn with a viewer key = %+v, %v; want a viewer session", sess, err)
	}
}

// TestSignInRefusesUnacceptedKeysAndKeepsThePresentedSession checks that a malformed,
// unknown and revoked key are all ErrUnauthenticated, and that a refused sign-in leaves
// the browser's existing session alone.
func TestSignInRefusesUnacceptedKeysAndKeepsThePresentedSession(t *testing.T) {
	fx := newSessionFixture(t)
	presented := fx.signIn(t, fx.opKey, "")
	revoked, revokedID := fx.db.addKey(t, "old", ScopeOperator)
	fx.db.keys[revokedID].revoked = true

	for name, key := range map[string]string{
		"malformed": "not-a-key",
		"unknown":   "cairn_" + strings.Repeat("A", 43),
		"revoked":   revoked,
	} {
		_, _, err := fx.sessions.SignIn(context.Background(), key, presented)
		if !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("SignIn with a %s key = %v, want ErrUnauthenticated", name, err)
		}
	}
	if _, err := fx.sessions.Check(context.Background(), presented); err != nil {
		t.Errorf("the presented session ended after a refused sign-in: %v", err)
	}
}

// TestSignInAgainDeletesThePresentedSession checks that a new sign-in ends the session
// the browser presented: the old token no longer checks.
func TestSignInAgainDeletesThePresentedSession(t *testing.T) {
	fx := newSessionFixture(t)
	old := fx.signIn(t, fx.opKey, "")
	fresh := fx.signIn(t, fx.opKey, old)
	if _, err := fx.sessions.Check(context.Background(), old); !errors.Is(err, ErrNoSession) {
		t.Errorf("the old token after a new sign-in = %v, want ErrNoSession", err)
	}
	if _, err := fx.sessions.Check(context.Background(), fresh); err != nil {
		t.Errorf("the new token = %v, want a live session", err)
	}
}

// TestAbsoluteLifetimeEndsASessionThatIsKeptActive checks the absolute end. The session
// is used every 30 minutes, so the idle end never comes first. It is live one
// nanosecond before 12 hours and ended at exactly 12 hours.
func TestAbsoluteLifetimeEndsASessionThatIsKeptActive(t *testing.T) {
	fx := newSessionFixture(t)
	token := fx.signIn(t, fx.opKey, "")
	start := fx.clock.t
	for step := 30 * time.Minute; step < SessionAbsolute; step += 30 * time.Minute {
		fx.clock.t = start.Add(step)
		if _, err := fx.sessions.Check(context.Background(), token); err != nil {
			t.Fatalf("at +%v a kept-active session = %v, want live", step, err)
		}
	}
	fx.clock.t = start.Add(SessionAbsolute - time.Nanosecond)
	if _, err := fx.sessions.Check(context.Background(), token); err != nil {
		t.Errorf("one nanosecond before the absolute end = %v, want live", err)
	}
	fx.clock.t = start.Add(SessionAbsolute)
	if _, err := fx.sessions.Check(context.Background(), token); !errors.Is(err, ErrNoSession) {
		t.Errorf("at the absolute end = %v, want ErrNoSession", err)
	}
	if len(fx.db.sessions) != 0 {
		t.Errorf("an ended session left %d row(s) behind", len(fx.db.sessions))
	}
}

// TestIdleLifetimeEndsAnIdleSession checks the idle end: a session not used for two
// hours is ended, and one used a minute before it is not.
func TestIdleLifetimeEndsAnIdleSession(t *testing.T) {
	fx := newSessionFixture(t)
	token := fx.signIn(t, fx.opKey, "")
	start := fx.clock.t

	fx.clock.t = start.Add(SessionIdle - time.Nanosecond)
	if _, err := fx.sessions.Check(context.Background(), token); err != nil {
		t.Fatalf("one nanosecond before the idle end = %v, want live", err)
	}
	// That check stamped last_seen_at at once, so the idle end moved with it.
	fx.clock.t = fx.clock.t.Add(SessionIdle - time.Nanosecond)
	if _, err := fx.sessions.Check(context.Background(), token); err != nil {
		t.Fatalf("a session used just before its idle end = %v, want live", err)
	}

	idleFrom := fx.clock.t
	fx.clock.t = idleFrom.Add(SessionIdle)
	if _, err := fx.sessions.Check(context.Background(), token); !errors.Is(err, ErrNoSession) {
		t.Errorf("at the idle end = %v, want ErrNoSession", err)
	}
}

// TestLastSeenMovesAtMostOncePerMinute checks the stored activity rule: a request within
// a minute of the last stamp does not write, and the first one at a minute or more does.
func TestLastSeenMovesAtMostOncePerMinute(t *testing.T) {
	fx := newSessionFixture(t)
	token := fx.signIn(t, fx.opKey, "")
	start := fx.clock.t
	for _, at := range []time.Duration{30 * time.Second, 61 * time.Second, 90 * time.Second, 122 * time.Second} {
		fx.clock.t = start.Add(at)
		if _, err := fx.sessions.Check(context.Background(), token); err != nil {
			t.Fatalf("Check at +%v: %v", at, err)
		}
	}
	want := []time.Time{start.Add(61 * time.Second), start.Add(122 * time.Second)}
	if len(fx.db.seenTouch) != len(want) {
		t.Fatalf("last_seen_at writes = %v, want %v", fx.db.seenTouch, want)
	}
	for i, w := range want {
		if !fx.db.seenTouch[i].Equal(w) {
			t.Errorf("write %d at %v, want %v", i, fx.db.seenTouch[i], w)
		}
	}
	if fx.db.keyTouch == 0 {
		t.Error("the key's last_used_at was never moved by a UI request")
	}
}

// TestFailedLastSeenWriteIsLoggedAndRefusesNothing checks that a failed stamp is
// reported, and the request still succeeds.
func TestFailedLastSeenWriteIsLoggedAndRefusesNothing(t *testing.T) {
	fx := newSessionFixture(t)
	token := fx.signIn(t, fx.opKey, "")
	fx.db.touchErr = errors.New("disk full")
	fx.clock.t = fx.clock.t.Add(2 * time.Minute)
	if _, err := fx.sessions.Check(context.Background(), token); err != nil {
		t.Fatalf("Check with a failing stamp = %v, want the session", err)
	}
	if len(fx.failures.got) != 1 || !strings.Contains(fx.failures.got[0].Error(), "disk full") {
		t.Errorf("reported failures = %v, want the stamp failure once", fx.failures.got)
	}
}

// TestRevokedKeyEndsItsSessionAtTheNextRequest checks that revoking a key ends its
// session at the next request, not at the expiry.
func TestRevokedKeyEndsItsSessionAtTheNextRequest(t *testing.T) {
	fx := newSessionFixture(t)
	token := fx.signIn(t, fx.opKey, "")
	if _, err := fx.sessions.Check(context.Background(), token); err != nil {
		t.Fatalf("before revocation: %v", err)
	}
	fx.db.keys[fx.opID].revoked = true
	if _, err := fx.sessions.Check(context.Background(), token); !errors.Is(err, ErrNoSession) {
		t.Errorf("after revocation = %v, want ErrNoSession", err)
	}
	if _, ok := fx.db.sessions[HashToken(token)]; ok {
		t.Error("the session of a revoked key was not deleted")
	}
}

// TestRoleIsTheKeysScopeNow checks that the role is read each request: a key whose
// scope becomes agent loses its session, and one whose scope becomes operator gains a
// role at once.
func TestRoleIsTheKeysScopeNow(t *testing.T) {
	fx := newSessionFixture(t)
	viewToken := fx.signIn(t, fx.viewKey, "")
	fx.db.keys[fx.viewID].scope = ScopeOperator
	got, err := fx.sessions.Check(context.Background(), viewToken)
	if err != nil || got.Key.Scope != ScopeOperator {
		t.Errorf("after promotion = %+v, %v; want operator", got, err)
	}
	fx.db.keys[fx.viewID].scope = ScopeAgent
	if _, err := fx.sessions.Check(context.Background(), viewToken); !errors.Is(err, ErrNoSession) {
		t.Errorf("after the key became an agent key = %v, want ErrNoSession", err)
	}
}

// TestEndSignsOutAndIsRepeatable checks sign-out: End deletes the session, a second End
// is not an error, and a malformed token deletes nothing.
func TestEndSignsOutAndIsRepeatable(t *testing.T) {
	fx := newSessionFixture(t)
	token := fx.signIn(t, fx.opKey, "")
	other := fx.signIn(t, fx.viewKey, "")
	if err := fx.sessions.End(context.Background(), token); err != nil {
		t.Fatalf("End: %v", err)
	}
	if _, err := fx.sessions.Check(context.Background(), token); !errors.Is(err, ErrNoSession) {
		t.Errorf("after sign-out = %v, want ErrNoSession", err)
	}
	if err := fx.sessions.End(context.Background(), token); err != nil {
		t.Errorf("a second End = %v, want nil", err)
	}
	if err := fx.sessions.End(context.Background(), "junk"); err != nil {
		t.Errorf("End of a malformed token = %v, want nil", err)
	}
	if _, err := fx.sessions.Check(context.Background(), other); err != nil {
		t.Errorf("another session after a sign-out = %v, want live", err)
	}
}

// TestEndReportsAFailedDelete checks that a failed delete on sign-out is returned, so
// the caller can say the sign-out did not finish.
func TestEndReportsAFailedDelete(t *testing.T) {
	fx := newSessionFixture(t)
	token := fx.signIn(t, fx.opKey, "")
	fx.db.deleteErr = errors.New("locked")
	if err := fx.sessions.End(context.Background(), token); err == nil {
		t.Error("End with a failing delete = nil, want the error")
	}
}

// TestMalformedTokensAreRefusedBeforeTheStore checks that a cookie of the wrong shape is
// ErrNoSession, and reads nothing.
func TestMalformedTokensAreRefusedBeforeTheStore(t *testing.T) {
	fx := newSessionFixture(t)
	for _, tok := range []string{"", "short", strings.Repeat("A", 42), strings.Repeat("A", 44), "!!!!" + strings.Repeat("A", 39)} {
		if _, err := fx.sessions.Check(context.Background(), tok); !errors.Is(err, ErrNoSession) {
			t.Errorf("Check(%q) = %v, want ErrNoSession", tok, err)
		}
	}
}

// TestSweepRunsAtSignInAndHourly checks the cleanup: a sign-in always cleans up, a
// request cleans up on the first call and then no more than once an hour.
func TestSweepRunsAtSignInAndHourly(t *testing.T) {
	fx := newSessionFixture(t)
	token := fx.signIn(t, fx.opKey, "")
	if fx.db.sweeps != 1 {
		t.Fatalf("sweeps after sign-in = %d, want 1", fx.db.sweeps)
	}
	start := fx.clock.t
	fx.clock.t = start.Add(10 * time.Minute)
	if _, err := fx.sessions.Check(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if fx.db.sweeps != 1 {
		t.Errorf("a request 10 minutes after the sweep swept again: %d", fx.db.sweeps)
	}
	fx.clock.t = start.Add(time.Hour)
	if _, err := fx.sessions.Check(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	if fx.db.sweeps != 2 {
		t.Errorf("a request an hour after the sweep: sweeps = %d, want 2", fx.db.sweeps)
	}
}

// TestSweepDeletesOnlyExpiredRows checks that the cleanup removes the expired row of a
// browser that never came back, and keeps the live one.
func TestSweepDeletesOnlyExpiredRows(t *testing.T) {
	fx := newSessionFixture(t)
	gone := fx.signIn(t, fx.opKey, "")
	fx.clock.t = fx.clock.t.Add(SessionIdle + time.Minute)
	live := fx.signIn(t, fx.viewKey, "")
	if _, ok := fx.db.sessions[HashToken(gone)]; ok {
		t.Error("the idle session survived the cleanup at the second sign-in")
	}
	if _, ok := fx.db.sessions[HashToken(live)]; !ok {
		t.Error("the live session was swept")
	}
}

// TestNewTokenIsFreshAndWellFormed checks the token shape and that two tokens differ.
func TestNewTokenIsFreshAndWellFormed(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 43 || !wellFormedToken(a) || a == b {
		t.Errorf("tokens %q and %q: want two distinct 43-character tokens", a, b)
	}
}
