package ui

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
)

// The tests here keep to the package's own rule: no store, no server, no httptest. So the
// key table and the session table are in memory, with the store's rules, and the
// responses are recorded by hand.

// fakeKey is a key row of the fake table.
type fakeKey struct {
	id      int64
	name    string
	scope   auth.Scope
	hash    string
	revoked bool
}

// fakeStore is the key table and the session table in memory. It is both the auth.Lookup
// and the auth.SessionStore of a session service.
type fakeStore struct {
	keys     map[int64]*fakeKey
	sessions map[string]auth.NewSession
}

// newFakeStore returns an empty fake store.
func newFakeStore() *fakeStore {
	return &fakeStore{keys: map[int64]*fakeKey{}, sessions: map[string]auth.NewSession{}}
}

// addKey adds an active key named name with scope, and returns its plaintext and id.
func (s *fakeStore) addKey(t *testing.T, name string, scope auth.Scope) (string, int64) {
	t.Helper()
	key, err := auth.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	id := int64(len(s.keys) + 1)
	s.keys[id] = &fakeKey{id: id, name: name, scope: scope, hash: auth.HashKey(key)}
	return key, id
}

// FindActive implements auth.Lookup: the unrevoked key with hash.
func (s *fakeStore) FindActive(_ context.Context, hash string) (auth.Key, error) {
	for _, k := range s.keys {
		if k.hash == hash && !k.revoked {
			return auth.Key{ID: k.id, Name: k.name, Scope: k.scope}, nil
		}
	}
	return auth.Key{}, fmt.Errorf("fake: %w", auth.ErrUnknownKey)
}

// Touch implements auth.Lookup. The UI tests do not read last_used_at.
func (s *fakeStore) Touch(context.Context, int64, time.Time) error { return nil }

// CreateSession implements auth.SessionStore.
func (s *fakeStore) CreateSession(_ context.Context, n auth.NewSession) error {
	s.sessions[n.TokenHash] = n
	return nil
}

// ReadSession implements auth.SessionStore: the row with its key's current state.
func (s *fakeStore) ReadSession(_ context.Context, hash string) (auth.SessionRecord, error) {
	n, ok := s.sessions[hash]
	if !ok {
		return auth.SessionRecord{}, fmt.Errorf("fake: %w", auth.ErrNoSession)
	}
	k := s.keys[n.KeyID]
	return auth.SessionRecord{
		TokenHash: hash, KeyID: k.id, KeyName: k.name, Scope: k.scope, Revoked: k.revoked,
		CreatedAt: n.CreatedAt, ExpiresAt: n.ExpiresAt, LastSeenAt: n.LastSeenAt,
	}, nil
}

// TouchSession implements auth.SessionStore.
func (s *fakeStore) TouchSession(_ context.Context, hash string, at time.Time) error {
	n, ok := s.sessions[hash]
	if !ok {
		return fmt.Errorf("fake: %w", auth.ErrNoSession)
	}
	n.LastSeenAt = at
	s.sessions[hash] = n
	return nil
}

// DeleteSession implements auth.SessionStore.
func (s *fakeStore) DeleteSession(_ context.Context, hash string) error {
	delete(s.sessions, hash)
	return nil
}

// DeleteExpired implements auth.SessionStore, on the store's two cutoffs.
func (s *fakeStore) DeleteExpired(_ context.Context, expiredBy, idleBefore time.Time) (int64, error) {
	var n int64
	for hash, row := range s.sessions {
		if !row.ExpiresAt.After(expiredBy) || !row.LastSeenAt.After(idleBefore) {
			delete(s.sessions, hash)
			n++
		}
	}
	return n, nil
}

// clock is the injected time of a test.
type clock struct{ t time.Time }

// now is the clock's time, as the session service reads it.
func (c *clock) now() time.Time { return c.t }

// recorder is the response writer of a test: it keeps the status, the headers and the
// body. A response with no WriteHeader is 200, as net/http sends it.
type recorder struct {
	h     http.Header
	code  int
	wrote bool
	body  bytes.Buffer
}

// newRecorder returns an empty recorder.
func newRecorder() *recorder {
	return &recorder{h: http.Header{}}
}

// Header is the response's header map.
func (r *recorder) Header() http.Header { return r.h }

// WriteHeader records the status the first time it is called.
func (r *recorder) WriteHeader(code int) {
	if !r.wrote {
		r.code = code
		r.wrote = true
	}
}

// Write records the body, with a 200 status if none was set.
func (r *recorder) Write(p []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	return r.body.Write(p)
}

// status is the response's status code.
func (r *recorder) status() int {
	if !r.wrote {
		return http.StatusOK
	}
	return r.code
}

// cookies returns the cookies the response sets.
func (r *recorder) cookies() []*http.Cookie {
	return (&http.Response{Header: r.h}).Cookies()
}

// harness is one UI over a fake store, a clock and a log, with keys already made.
type harness struct {
	t        *testing.T
	store    *fakeStore
	clock    *clock
	logs     *bytes.Buffer
	failures []error
	cfg      Config
	h        http.Handler // the UI as the server mounts it
	u        *ui          // the same UI, for the form chain
	opKey    string
	viewKey  string
	agentKey string
	viewID   int64
	opID     int64
}

// host is the host name of every request: the Host header and the Origin of a same-origin
// form, with the scheme the harness is configured for.
const host = "cairn.test"

// newHarness builds a UI. secure sets the Secure flag (TLS or behind a proxy); hsts sets
// the HSTS flag. The clock starts at 9:00 UTC on 2026-11-03.
func newHarness(t *testing.T, secure, hsts bool) *harness {
	t.Helper()
	st := newFakeStore()
	op, opID := st.addKey(t, "ops", auth.ScopeOperator)
	view, viewID := st.addKey(t, "watch", auth.ScopeViewer)
	agent, _ := st.addKey(t, "claude", auth.ScopeAgent)
	c := &clock{t: time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)}
	hr := &harness{t: t, store: st, clock: c, logs: &bytes.Buffer{}, opKey: op, viewKey: view,
		agentKey: agent, opID: opID, viewID: viewID}
	logger := slog.New(slog.NewTextHandler(hr.logs, nil))
	sessions := auth.NewSessions(st, auth.NewResolver(st, nil), c.now, func(err error) {
		hr.failures = append(hr.failures, err)
	})
	hr.cfg = Config{Sessions: sessions, Secure: secure, HSTS: hsts, Logger: logger}
	hr.h = New(hr.cfg)
	hr.u = newUI(hr.cfg)
	return hr
}

// origin is the Origin a same-origin form sends: the scheme the harness is set for.
func (hr *harness) origin() string {
	if hr.cfg.Secure {
		return "https://" + host
	}
	return "http://" + host
}

// request builds a request for path with the given method, headers and form body, and the
// cookie when one is given. A body is sent as a form.
func (hr *harness) request(method, path, body string, cookie *http.Cookie, hdr map[string]string) *http.Request {
	hr.t.Helper()
	r, err := http.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	if err != nil {
		hr.t.Fatal(err)
	}
	r.Host = host
	if body != "" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return r
}

// do sends a request through the UI and returns the recorded answer.
func (hr *harness) do(method, path, body string, cookie *http.Cookie, hdr map[string]string) *recorder {
	hr.t.Helper()
	rec := newRecorder()
	hr.h.ServeHTTP(rec, hr.request(method, path, body, cookie, hdr))
	return rec
}

// signInHeaders are the headers of a same-origin sign-in form.
func (hr *harness) signInHeaders() map[string]string {
	return map[string]string{"Origin": hr.origin()}
}

// signIn signs key in through the form and returns the session cookie. It fails the test
// when the answer is not the redirect to the dashboard.
func (hr *harness) signIn(key string, presented *http.Cookie) *http.Cookie {
	hr.t.Helper()
	rec := hr.do(http.MethodPost, "/ui/signin", "key="+key, presented, hr.signInHeaders())
	if rec.status() != http.StatusSeeOther {
		hr.t.Fatalf("sign-in status %d, want 303: %s", rec.status(), rec.body.String())
	}
	for _, c := range rec.cookies() {
		if c.Name == hr.cookieName() {
			return c
		}
	}
	hr.t.Fatalf("sign-in set no %s cookie", hr.cookieName())
	return nil
}

// cookieName is the session cookie's name for this harness.
func (hr *harness) cookieName() string {
	if hr.cfg.Secure {
		return "__Secure-cairn_ui"
	}
	return "cairn_ui"
}

// csrfFor returns the CSRF value of the session cookie c, as a rendered form carries it.
func csrfFor(c *http.Cookie) string {
	return csrfToken(c.Value)
}

// Compile-time checks that the fakes meet the interfaces the UI is built over.
var (
	_ auth.Lookup       = (*fakeStore)(nil)
	_ auth.SessionStore = (*fakeStore)(nil)
)

// bodyTrap is a request body that fails the test if anything reads it. It proves a
// refusal that comes before the body is read.
type bodyTrap struct {
	t    *testing.T
	read bool
}

// Read fails the test: the body must not be read on this path.
func (b *bodyTrap) Read([]byte) (int, error) {
	b.read = true
	b.t.Errorf("the request body was read on a path that must refuse before reading it")
	return 0, fmt.Errorf("trap")
}

// Close is a no-op.
func (b *bodyTrap) Close() error { return nil }

// logText is what the UI logged so far.
func (hr *harness) logText() string { return hr.logs.String() }

// encodeHMAC returns HMAC-SHA256 of label keyed by token, in unpadded base64url. The
// tests use it to build a CSRF value the server must not accept: one under another label.
func encodeHMAC(token, label string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(label))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
