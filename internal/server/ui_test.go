package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/store"
)

// noRedirect returns a client that reports a redirect as the answer, so a test sees the 303
// and its Location and cookie, not the page it leads to.
func noRedirect() *http.Client {
	return &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// postForm sends a form POST with the given headers and returns the answer and its body.
// send is for JSON; a form needs its own content type, or the server reads no fields.
func postForm(t *testing.T, c *http.Client, target string, form url.Values, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, out
}

// uiCookie returns the session cookie of resp, by name, or nil.
func uiCookie(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// TestUISignInAndSessionOverARealListener runs the flow over a listener and the real store:
// sign-in, the dashboard, and sign-out. The store keeps the hash of the token and not the
// token itself, and the dashboard answers only with the cookie.
func TestUISignInAndSessionOverARealListener(t *testing.T) {
	st := openStore(t)
	key := addKey(t, st, "ops", auth.ScopeOperator)
	base := startServer(t, Handler(st, testLogger(io.Discard)), nil)
	c := noRedirect()

	resp, _ := postForm(t, c, base+"/ui/signin", url.Values{"key": {key}}, map[string]string{"Origin": base})
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/ui/" {
		t.Fatalf("sign-in: status %d location %q, want 303 to /ui/", resp.StatusCode, resp.Header.Get("Location"))
	}
	cookie := uiCookie(resp, "cairn_ui")
	if cookie == nil {
		t.Fatalf("sign-in set no cairn_ui cookie: %v", resp.Header["Set-Cookie"])
	}

	dash := get(t, c, base+"/ui/", cookie)
	if dash.StatusCode != http.StatusOK {
		t.Fatalf("dashboard: status %d", dash.StatusCode)
	}

	var stored string
	if err := st.QueryRowContext(context.Background(), `SELECT token_hash FROM ui_sessions`).Scan(&stored); err != nil {
		t.Fatalf("read the session row: %v", err)
	}
	if stored != auth.HashToken(cookie.Value) {
		t.Errorf("stored token_hash = %q, want the SHA-256 of the cookie token", stored)
	}
	if stored == cookie.Value {
		t.Error("the raw token is stored")
	}

	out := get(t, c, base+"/ui/signout", cookie)
	if out.StatusCode != http.StatusSeeOther {
		t.Errorf("sign-out status %d, want 303", out.StatusCode)
	}
	if after := get(t, c, base+"/ui/", cookie); after.StatusCode != http.StatusSeeOther {
		t.Errorf("dashboard after sign-out: status %d, want a redirect", after.StatusCode)
	}
}

// get sends a GET with an optional session cookie and returns the answer, with its body
// drained and closed.
func get(t *testing.T, c *http.Client, target string, cookie *http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp
}

// TestUIKeysStayOutOfTheLogAndTheAnswers runs sign-in with a good key, a refused agent key
// and an unknown key, over the real logger, and checks that no key is in a log line or in
// an answer.
func TestUIKeysStayOutOfTheLogAndTheAnswers(t *testing.T) {
	st := openStore(t)
	op := addKey(t, st, "ops", auth.ScopeOperator)
	agent := addKey(t, st, "claude", auth.ScopeAgent)
	unknown := "cairn_" + strings.Repeat("E", 43)
	var logs syncBuffer
	base := startServer(t, Handler(st, testLogger(&logs)), nil)
	c := noRedirect()

	var bodies []string
	for _, key := range []string{op, agent, unknown} {
		resp, body := postForm(t, c, base+"/ui/signin", url.Values{"key": {key}}, map[string]string{"Origin": base})
		bodies = append(bodies, string(body))
		for name, vals := range resp.Header {
			for _, v := range vals {
				if strings.Contains(v, key) {
					t.Errorf("header %s carries a key", name)
				}
			}
		}
	}
	for _, key := range []string{op, agent, unknown} {
		for _, b := range bodies {
			if strings.Contains(b, key) {
				t.Errorf("an answer carries a key")
			}
		}
		if strings.Contains(logs.String(), key) {
			t.Errorf("the log carries a key: %s", logs.String())
		}
	}
}

// TestUICookieNameAndSecureFlagFollowTheTransport checks the cookie over the transport: plain
// for a server with no TLS and no proxy, and __Secure- with Secure for TLS and for a proxy.
func TestUICookieNameAndSecureFlagFollowTheTransport(t *testing.T) {
	cases := []struct {
		label  string
		tr     Transport
		name   string
		secure bool
		scheme string
	}{
		{"plain loopback", Transport{Loopback: true}, "cairn_ui", false, "http"},
		{"behind a proxy", Transport{BehindProxy: true}, "__Secure-cairn_ui", true, "https"},
		{"TLS on loopback", Transport{TLS: true, Loopback: true}, "__Secure-cairn_ui", true, "https"},
	}
	for _, c := range cases {
		st := openStore(t)
		key := addKey(t, st, "ops", auth.ScopeOperator)
		h := NewHandler(st, testLogger(io.Discard), c.tr)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/ui/signin", strings.NewReader(url.Values{"key": {key}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", c.scheme+"://example.com")
		req.Host = "example.com"
		h.ServeHTTP(rec, req)
		resp := rec.Result()
		cookie := uiCookie(resp, c.name)
		if cookie == nil {
			t.Errorf("%s: no %s cookie: %v", c.label, c.name, resp.Header["Set-Cookie"])
			continue
		}
		if cookie.Secure != c.secure || !cookie.HttpOnly || cookie.Path != "/ui" {
			t.Errorf("%s: cookie Secure %v HttpOnly %v Path %q, want Secure %v, HttpOnly, /ui",
				c.label, cookie.Secure, cookie.HttpOnly, cookie.Path, c.secure)
		}
	}
}

// TestHSTSIsSentOnlyForTLSOnANonLoopbackAddress checks the HSTS rule (N2). It is sent only
// when the server serves TLS itself on a non-loopback address. It is not sent for loopback,
// which a trusted local certificate would otherwise hold on every port of the host, and it is
// not sent behind a proxy, which sets its own.
func TestHSTSIsSentOnlyForTLSOnANonLoopbackAddress(t *testing.T) {
	cases := []struct {
		label string
		tr    Transport
		hsts  bool
	}{
		{"TLS, not loopback", Transport{TLS: true}, true},
		{"TLS, loopback", Transport{TLS: true, Loopback: true}, false},
		{"behind a proxy", Transport{BehindProxy: true}, false},
		{"plain", Transport{}, false},
	}
	for _, c := range cases {
		st := openStore(t)
		h := NewHandler(st, testLogger(io.Discard), c.tr)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/signin", nil))
		got := rec.Header().Get("Strict-Transport-Security")
		if c.hsts && got != "max-age=31536000" {
			t.Errorf("%s: Strict-Transport-Security = %q, want max-age=31536000", c.label, got)
		}
		if !c.hsts && got != "" {
			t.Errorf("%s: Strict-Transport-Security = %q, want none", c.label, got)
		}
	}
}

// TestUIHeadersCoverTheMuxAnswersThroughTheWholeHandler checks the headers on the answers the
// full handler makes for /ui: the redirect from /ui, a 404 under /ui/, and a 405. The
// body cap of the server wraps all of them, so the check runs through it.
func TestUIHeadersCoverTheMuxAnswersThroughTheWholeHandler(t *testing.T) {
	st := openStore(t)
	h := NewHandler(st, testLogger(io.Discard), Transport{})
	for _, c := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/ui", http.StatusMovedPermanently},
		{http.MethodGet, "/ui/nope", http.StatusNotFound},
		{http.MethodPost, "/ui/", http.StatusMethodNotAllowed},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
		if rec.Code != c.status {
			t.Errorf("%s %s: status %d, want %d", c.method, c.path, rec.Code, c.status)
		}
		if rec.Header().Get("Content-Security-Policy") == "" || rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
			rec.Header().Get("Referrer-Policy") != "same-origin" || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s: the security headers are missing: %v", c.method, c.path, rec.Header())
		}
	}
}

// TestRevokedKeyEndsItsUISessionOnTheRunningServer revokes a key in the real store while
// its browser holds a session, and checks that the next request ends the session.
func TestRevokedKeyEndsItsUISessionOnTheRunningServer(t *testing.T) {
	st := openStore(t)
	key := addKey(t, st, "ops", auth.ScopeOperator)
	base := startServer(t, Handler(st, testLogger(io.Discard)), nil)
	c := noRedirect()
	resp, _ := postForm(t, c, base+"/ui/signin", url.Values{"key": {key}}, map[string]string{"Origin": base})
	cookie := uiCookie(resp, "cairn_ui")
	if cookie == nil {
		t.Fatal("no session cookie")
	}
	if got := get(t, c, base+"/ui/", cookie); got.StatusCode != http.StatusOK {
		t.Fatalf("before revocation: status %d", got.StatusCode)
	}
	if err := st.WithTx(context.Background(), func(q store.Querier) error {
		return store.RevokeAPIKey(context.Background(), q, "ops", time.Now())
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got := get(t, c, base+"/ui/", cookie); got.StatusCode != http.StatusSeeOther {
		t.Errorf("after revocation: status %d, want a redirect to sign-in", got.StatusCode)
	}
}

// TestSessionStoreAdapterRoundTrip checks the adapter over the real table: create, read with
// the key's state, touch, delete, and the cleanup on both cutoffs, with the not-found answers
// the session rules expect.
func TestSessionStoreAdapterRoundTrip(t *testing.T) {
	st := openStore(t)
	addKey(t, st, "ops", auth.ScopeOperator)
	ctx := context.Background()
	var keyID int64
	if err := st.QueryRowContext(ctx, `SELECT id FROM api_keys WHERE name = 'ops'`).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	ss := NewSessionStore(st)
	t0 := time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)
	if err := ss.CreateSession(ctx, auth.NewSession{TokenHash: "h1", KeyID: keyID,
		CreatedAt: t0, ExpiresAt: t0.Add(12 * time.Hour), LastSeenAt: t0}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	rec, err := ss.ReadSession(ctx, "h1")
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if rec.KeyName != "ops" || rec.Scope != auth.ScopeOperator || rec.Revoked || rec.KeyID != keyID {
		t.Errorf("ReadSession = %+v, want the ops operator key, not revoked", rec)
	}
	if _, err := ss.ReadSession(ctx, "missing"); err == nil || !strings.Contains(err.Error(), auth.ErrNoSession.Error()) {
		t.Errorf("ReadSession of a missing row = %v, want ErrNoSession", err)
	}
	if err := ss.TouchSession(ctx, "h1", t0.Add(time.Minute)); err != nil {
		t.Errorf("TouchSession: %v", err)
	}
	if err := ss.TouchSession(ctx, "missing", t0); err == nil || !strings.Contains(err.Error(), auth.ErrNoSession.Error()) {
		t.Errorf("TouchSession of a missing row = %v, want ErrNoSession", err)
	}
	n, err := ss.DeleteExpired(ctx, t0.Add(13*time.Hour), t0.Add(11*time.Hour))
	if err != nil || n != 1 {
		t.Errorf("DeleteExpired past the absolute end = %d, %v; want 1", n, err)
	}
	if err := ss.DeleteSession(ctx, "h1"); err != nil {
		t.Errorf("DeleteSession of a row already gone: %v", err)
	}
}
