package ui

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
)

// The security headers of DESIGN §5.5, as every answer under /ui/ carries them.
const wantCSP = "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

// checkHeaders fails the test unless rec carries the four headers that every answer has,
// and HSTS exactly when hsts is set.
func checkHeaders(t *testing.T, label string, rec *recorder, hsts bool) {
	t.Helper()
	want := map[string]string{
		"Content-Security-Policy": wantCSP,
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "same-origin",
		"Cache-Control":           "no-store",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s: %s = %q, want %q", label, k, got, v)
		}
	}
	got := rec.Header().Get("Strict-Transport-Security")
	if hsts && got != "max-age=31536000" {
		t.Errorf("%s: Strict-Transport-Security = %q, want max-age=31536000", label, got)
	}
	if !hsts && got != "" {
		t.Errorf("%s: Strict-Transport-Security = %q, want none", label, got)
	}
}

// TestHeadersOnEveryAnswer checks the headers on a page, on the redirects, on the stylesheet,
// and on the mux's own 404 and 405 answers. It runs with and without HSTS.
func TestHeadersOnEveryAnswer(t *testing.T) {
	for _, hsts := range []bool{false, true} {
		hr := newHarness(t, true, hsts)
		cases := []struct {
			label, method, path string
			status              int
		}{
			{"sign-in page", http.MethodGet, "/ui/signin", http.StatusOK},
			{"dashboard with no session", http.MethodGet, "/ui/", http.StatusSeeOther},
			{"the /ui redirect", http.MethodGet, "/ui", http.StatusMovedPermanently},
			{"the stylesheet", http.MethodGet, "/ui/static/app.css", http.StatusOK},
			{"an unlisted path (404)", http.MethodGet, "/ui/nope", http.StatusNotFound},
			{"a wrong method (405)", http.MethodPost, "/ui/", http.StatusMethodNotAllowed},
			{"a sign-in with no body", http.MethodPost, "/ui/signin", http.StatusForbidden},
			{"HEAD of the sign-in page", http.MethodHead, "/ui/signin", http.StatusOK},
			{"a trailing slash on the sign-in page (404)", http.MethodGet, "/ui/signin/", http.StatusNotFound},
			{"a trailing slash on the stylesheet (404)", http.MethodGet, "/ui/static/app.css/", http.StatusNotFound},
		}
		for _, c := range cases {
			rec := hr.do(c.method, c.path, "", nil, nil)
			if rec.status() != c.status {
				t.Errorf("%s: status %d, want %d", c.label, rec.status(), c.status)
			}
			checkHeaders(t, c.label, rec, hsts)
		}
		if got := hr.do(http.MethodPost, "/ui/", "", nil, nil).Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("405 Allow = %q, want GET, HEAD", got)
		}
	}
}

// TestStylesheetIsServedAsCSS checks the one stylesheet: its type, and that it is the
// embedded file.
func TestStylesheetIsServedAsCSS(t *testing.T) {
	hr := newHarness(t, false, false)
	rec := hr.do(http.MethodGet, "/ui/static/app.css", "", nil, nil)
	if got := rec.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if !strings.Contains(rec.body.String(), ".error") {
		t.Error("the stylesheet is not the embedded one")
	}
}

// TestAgentKeyIsRefusedAtSignIn checks the agent refusal: 401, the agent message, no
// cookie, and no session row.
func TestAgentKeyIsRefusedAtSignIn(t *testing.T) {
	hr := newHarness(t, false, false)
	rec := hr.do(http.MethodPost, "/ui/signin", "key="+hr.agentKey, nil, hr.signInHeaders())
	if rec.status() != http.StatusUnauthorized {
		t.Errorf("agent sign-in status %d, want 401", rec.status())
	}
	if !strings.Contains(rec.body.String(), "This is an agent key.") {
		t.Errorf("agent refusal does not say so: %s", rec.body.String())
	}
	if len(rec.cookies()) != 0 {
		t.Errorf("a refused agent got cookies: %v", rec.cookies())
	}
	if len(hr.store.sessions) != 0 {
		t.Errorf("a refused agent made %d session rows", len(hr.store.sessions))
	}
}

// TestOperatorAndViewerKeysSignIn checks that both roles sign in, redirect to the dashboard,
// and that the dashboard names the key and its role.
func TestOperatorAndViewerKeysSignIn(t *testing.T) {
	hr := newHarness(t, false, false)
	for _, c := range []struct{ key, name, role string }{
		{hr.opKey, "ops", "operator"},
		{hr.viewKey, "watch", "viewer"},
	} {
		rec := hr.do(http.MethodPost, "/ui/signin", "key="+c.key, nil, hr.signInHeaders())
		if rec.status() != http.StatusSeeOther || rec.Header().Get("Location") != "/ui/" {
			t.Fatalf("%s sign-in: status %d location %q, want 303 to /ui/", c.role, rec.status(), rec.Header().Get("Location"))
		}
		cookie := hr.signIn(c.key, nil)
		page := hr.do(http.MethodGet, "/ui/", "", cookie, nil)
		if page.status() != http.StatusOK {
			t.Fatalf("%s dashboard status %d", c.role, page.status())
		}
		// The header names the key and its role.
		want := c.name + " (" + c.role + ")"
		if !strings.Contains(page.body.String(), want) {
			t.Errorf("header does not say %q: %s", want, page.body.String())
		}
	}
}

// TestSignInTrimsWhiteSpaceAndRefusesTheRest checks that spaces at the ends of the key do
// not matter, and that a key with no prefix or a wrong one is refused without an echo.
func TestSignInTrimsWhiteSpaceAndRefusesTheRest(t *testing.T) {
	hr := newHarness(t, false, false)
	rec := hr.do(http.MethodPost, "/ui/signin", "key=%20"+hr.opKey+"%20", nil, hr.signInHeaders())
	if rec.status() != http.StatusSeeOther {
		t.Errorf("a padded key: status %d, want 303", rec.status())
	}
	for _, bad := range []string{"not-a-key", "cairn_" + strings.Repeat("B", 43)} {
		rec := hr.do(http.MethodPost, "/ui/signin", "key="+bad, nil, hr.signInHeaders())
		if rec.status() != http.StatusUnauthorized || !strings.Contains(rec.body.String(), "The key was not accepted.") {
			t.Errorf("bad key %q: status %d, body %q", bad, rec.status(), rec.body.String())
		}
		if strings.Contains(rec.body.String(), bad) {
			t.Errorf("the refusal echoes the key %q", bad)
		}
	}
}

// TestKeyNeverAppearsInAnyAnswerOrLog checks that a key does not reach a response body, a
// header or a log line, on success, on refusal, and on the dashboard after sign-in.
func TestKeyNeverAppearsInAnyAnswerOrLog(t *testing.T) {
	hr := newHarness(t, false, false)
	var answers []*recorder
	answers = append(answers, hr.do(http.MethodPost, "/ui/signin", "key="+hr.opKey, nil, hr.signInHeaders()))
	answers = append(answers, hr.do(http.MethodPost, "/ui/signin", "key="+hr.agentKey, nil, hr.signInHeaders()))
	answers = append(answers, hr.do(http.MethodPost, "/ui/signin", "key=cairn_"+strings.Repeat("C", 43), nil, hr.signInHeaders()))
	cookie := hr.signIn(hr.viewKey, nil)
	answers = append(answers, hr.do(http.MethodGet, "/ui/", "", cookie, nil))
	answers = append(answers, hr.do(http.MethodGet, "/ui/signin", "", nil, nil))
	for _, key := range []string{hr.opKey, hr.agentKey, hr.viewKey} {
		for i, a := range answers {
			if strings.Contains(a.body.String(), key) {
				t.Errorf("answer %d carries the key %q in its body", i, key)
			}
			for name, vals := range a.Header() {
				for _, v := range vals {
					if strings.Contains(v, key) {
						t.Errorf("answer %d carries the key in header %s", i, name)
					}
				}
			}
		}
		if strings.Contains(hr.logText(), key) {
			t.Errorf("the log carries the key %q: %s", key, hr.logText())
		}
	}
}

// TestSignInPageIsAPasswordFieldNamedKey checks the form: a password field named key, with
// autocomplete off, posting to /ui/signin.
func TestSignInPageIsAPasswordFieldNamedKey(t *testing.T) {
	hr := newHarness(t, false, false)
	body := hr.do(http.MethodGet, "/ui/signin", "", nil, nil).body.String()
	for _, want := range []string{`type="password"`, `name="key"`, `autocomplete="off"`, `action="/ui/signin"`} {
		if !strings.Contains(body, want) {
			t.Errorf("sign-in page lacks %s", want)
		}
	}
}

// TestCookieFlagsWithoutSecure checks the cookie of a plain-HTTP loopback server: the plain
// name, HttpOnly, SameSite=Strict, Path=/ui, not Secure, and no Max-Age or Expires.
func TestCookieFlagsWithoutSecure(t *testing.T) {
	hr := newHarness(t, false, false)
	rec := hr.do(http.MethodPost, "/ui/signin", "key="+hr.opKey, nil, hr.signInHeaders())
	c := cookieNamed(t, rec, "cairn_ui")
	checkCookie(t, c, false)
}

// TestCookieFlagsWithSecure checks the cookie under TLS or a proxy: the __Secure- name, and
// the same flags with Secure set.
func TestCookieFlagsWithSecure(t *testing.T) {
	hr := newHarness(t, true, false)
	rec := hr.do(http.MethodPost, "/ui/signin", "key="+hr.opKey, nil, hr.signInHeaders())
	c := cookieNamed(t, rec, "__Secure-cairn_ui")
	checkCookie(t, c, true)
}

// cookieNamed returns the cookie of rec with name, and fails the test when there is none.
func cookieNamed(t *testing.T, rec *recorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range rec.cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no cookie %s in %v", name, rec.Header()["Set-Cookie"])
	return nil
}

// checkCookie fails the test unless c has the flags of DESIGN §5.5, with Secure as given.
func checkCookie(t *testing.T, c *http.Cookie, secure bool) {
	t.Helper()
	if c.Path != "/ui" {
		t.Errorf("Path = %q, want /ui", c.Path)
	}
	if !c.HttpOnly {
		t.Error("the cookie is not HttpOnly")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v, want Strict", c.SameSite)
	}
	if c.Secure != secure {
		t.Errorf("Secure = %v, want %v", c.Secure, secure)
	}
	if c.MaxAge != 0 || !c.Expires.IsZero() {
		t.Errorf("a browser session cookie has Max-Age %d and Expires %v, want neither", c.MaxAge, c.Expires)
	}
}

// TestSignedInBrowserIsSentToTheDashboard checks that the sign-in page of a browser that
// holds a live session redirects, and that a refused sign-in keeps that session.
func TestSignedInBrowserIsSentToTheDashboard(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	rec := hr.do(http.MethodGet, "/ui/signin", "", cookie, nil)
	if rec.status() != http.StatusSeeOther || rec.Header().Get("Location") != "/ui/" {
		t.Errorf("sign-in page for a signed-in browser: status %d, location %q", rec.status(), rec.Header().Get("Location"))
	}
	refused := hr.do(http.MethodPost, "/ui/signin", "key=cairn_"+strings.Repeat("D", 43), cookie, hr.signInHeaders())
	if refused.status() != http.StatusUnauthorized {
		t.Fatalf("refused sign-in status %d", refused.status())
	}
	if rec := hr.do(http.MethodGet, "/ui/", "", cookie, nil); rec.status() != http.StatusOK {
		t.Errorf("a refused sign-in ended the browser's session: dashboard status %d", rec.status())
	}
}

// TestSignInAgainDeletesTheOldSession checks that a second sign-in from the same browser
// ends the first session: its cookie no longer reaches the dashboard.
func TestSignInAgainDeletesTheOldSession(t *testing.T) {
	hr := newHarness(t, false, false)
	old := hr.signIn(hr.opKey, nil)
	fresh := hr.signIn(hr.viewKey, old)
	if rec := hr.do(http.MethodGet, "/ui/", "", old, nil); rec.status() != http.StatusSeeOther {
		t.Errorf("the old session after a new sign-in: status %d, want a redirect to sign-in", rec.status())
	}
	if rec := hr.do(http.MethodGet, "/ui/", "", fresh, nil); rec.status() != http.StatusOK {
		t.Errorf("the new session: status %d, want 200", rec.status())
	}
	if len(hr.store.sessions) != 1 {
		t.Errorf("session rows = %d, want one", len(hr.store.sessions))
	}
}

// TestSignOutEndsTheSessionAndClearsTheCookie checks sign-out: the redirect to sign-in,
// the cookie cleared with Max-Age=0 under the same name, path and flags, and the old token
// dead afterwards.
func TestSignOutEndsTheSessionAndClearsTheCookie(t *testing.T) {
	hr := newHarness(t, true, false)
	cookie := hr.signIn(hr.opKey, nil)
	rec := hr.do(http.MethodGet, "/ui/signout", "", cookie, nil)
	if rec.status() != http.StatusSeeOther || rec.Header().Get("Location") != "/ui/signin" {
		t.Errorf("sign-out: status %d, location %q", rec.status(), rec.Header().Get("Location"))
	}
	cleared := cookieNamed(t, rec, "__Secure-cairn_ui")
	if cleared.Value != "" {
		t.Errorf("the cleared cookie carries value %q, want none", cleared.Value)
	}
	// Go reads "Max-Age=0" back as a negative MaxAge, so the header text is checked here.
	setCookie := rec.Header().Get("Set-Cookie")
	for _, want := range []string{"__Secure-cairn_ui=", "Max-Age=0", "Path=/ui", "HttpOnly", "SameSite=Strict", "Secure"} {
		if !strings.Contains(setCookie, want) {
			t.Errorf("the clearing Set-Cookie %q lacks %s", setCookie, want)
		}
	}
	if after := hr.do(http.MethodGet, "/ui/", "", cookie, nil); after.status() != http.StatusSeeOther {
		t.Errorf("the dashboard after sign-out: status %d, want a redirect", after.status())
	}
}

// TestGETWithNoSessionRedirectsWithNoNext checks that a page with no session goes to sign-in,
// and that the redirect carries no next parameter.
func TestGETWithNoSessionRedirectsWithNoNext(t *testing.T) {
	hr := newHarness(t, false, false)
	rec := hr.do(http.MethodGet, "/ui/", "", nil, nil)
	if rec.status() != http.StatusSeeOther || rec.Header().Get("Location") != "/ui/signin" {
		t.Errorf("no session: status %d, location %q, want 303 to /ui/signin", rec.status(), rec.Header().Get("Location"))
	}
}

// TestRevokedKeyEndsItsSessionAtTheNextRequest checks that revoking a key ends the session
// of the browser that signed in with it, at its next request.
func TestRevokedKeyEndsItsSessionAtTheNextRequest(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	if rec := hr.do(http.MethodGet, "/ui/", "", cookie, nil); rec.status() != http.StatusOK {
		t.Fatalf("before revocation: status %d", rec.status())
	}
	hr.store.keys[hr.opID].revoked = true
	if rec := hr.do(http.MethodGet, "/ui/", "", cookie, nil); rec.status() != http.StatusSeeOther {
		t.Errorf("after revocation: status %d, want a redirect to sign-in", rec.status())
	}
}

// TestAbsoluteAndIdleExpiryEndTheSession checks both lifetimes through the UI: a session
// used every 30 minutes is ended at 12 hours, and an idle one at 2 hours.
func TestAbsoluteAndIdleExpiryEndTheSession(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	start := hr.clock.t
	for step := 30 * time.Minute; step < auth.SessionAbsolute; step += 30 * time.Minute {
		hr.clock.t = start.Add(step)
		if rec := hr.do(http.MethodGet, "/ui/", "", cookie, nil); rec.status() != http.StatusOK {
			t.Fatalf("at +%v: status %d, want 200", step, rec.status())
		}
	}
	hr.clock.t = start.Add(auth.SessionAbsolute)
	if rec := hr.do(http.MethodGet, "/ui/", "", cookie, nil); rec.status() != http.StatusSeeOther {
		t.Errorf("at the absolute end: status %d, want a redirect", rec.status())
	}

	idle := hr.signIn(hr.viewKey, nil)
	idleStart := hr.clock.t
	hr.clock.t = idleStart.Add(auth.SessionIdle)
	if rec := hr.do(http.MethodGet, "/ui/", "", idle, nil); rec.status() != http.StatusSeeOther {
		t.Errorf("at the idle end: status %d, want a redirect", rec.status())
	}
}

// TestPOSTWithoutASessionRedirectsAndRunsNothing checks the first step of the form chain: no
// session is a redirect to sign-in, and the action does not run.
func TestPOSTWithoutASessionRedirectsAndRunsNothing(t *testing.T) {
	hr := newHarness(t, false, false)
	ran := false
	chain := hr.u.form(func(http.ResponseWriter, *http.Request, signedIn) { ran = true })
	rec := newRecorder()
	chain.ServeHTTP(rec, hr.request(http.MethodPost, "/ui/x", "csrf=x", nil, hr.signInHeaders()))
	if rec.status() != http.StatusSeeOther || rec.Header().Get("Location") != "/ui/signin" {
		t.Errorf("POST with no session: status %d, location %q", rec.status(), rec.Header().Get("Location"))
	}
	if ran {
		t.Error("the action ran with no session")
	}
}

// TestSessionIsCheckedBeforeOrigin pins the order of DESIGN §5.5 (Forms, the form chain): a POST with
// no session and a bad Origin is answered by the session check, a redirect to sign-in. The
// origin is not checked, so nothing is logged about it, and the action does not run.
func TestSessionIsCheckedBeforeOrigin(t *testing.T) {
	hr := newHarness(t, false, false)
	ran := false
	chain := hr.u.form(func(http.ResponseWriter, *http.Request, signedIn) { ran = true })
	rec := newRecorder()
	chain.ServeHTTP(rec, hr.request(http.MethodPost, "/ui/x", "csrf=x", nil, map[string]string{"Origin": "http://evil.test"}))
	if rec.status() != http.StatusSeeOther || rec.Header().Get("Location") != "/ui/signin" {
		t.Errorf("POST with no session and a bad origin: status %d, location %q, want 303 to sign-in",
			rec.status(), rec.Header().Get("Location"))
	}
	if ran {
		t.Error("the action ran with no session")
	}
	if logs := hr.logText(); strings.Contains(logs, "form refused") {
		t.Errorf("the origin was checked before the session: %q", logs)
	}
}

// TestBodyOverTheCapIsRefusedBeforeCSRF pins the order of DESIGN §5.5 (the form chain): a body
// over 64 KiB is 413 whether or not its csrf field is right, since the body is read before the
// field is compared. The field here is wrong on purpose.
func TestBodyOverTheCapIsRefusedBeforeCSRF(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	big := "csrf=not-the-token&pad=" + strings.Repeat("x", formBodyCap)
	rec, ran := formCase(hr, cookie, map[string]string{"Origin": hr.origin()}, big)
	if ran || rec.status() != http.StatusRequestEntityTooLarge {
		t.Errorf("a form over 64 KiB with a bad csrf: ran %v status %d, want 413 before the field is checked", ran, rec.status())
	}
}

// TestLoggedPathIsQuotedAndCapped checks that the path a refusal logs is quoted, so a line break
// in it cannot start a line of its own, and cut to logCap bytes, so a long path cannot fill the
// log. A cut is marked with "...".
func TestLoggedPathIsQuotedAndCapped(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	chain := hr.u.form(func(http.ResponseWriter, *http.Request, signedIn) {})
	rec := newRecorder()
	chain.ServeHTTP(rec, hr.request(http.MethodPost, "/ui/x%0AFAKE-LOG-LINE", "csrf="+csrfFor(cookie), cookie,
		map[string]string{"Origin": "http://evil.test"}))
	if rec.status() != http.StatusForbidden {
		t.Fatalf("refused origin: status %d, want 403", rec.status())
	}
	logs := hr.logText()
	if strings.Contains(logs, "\nFAKE-LOG-LINE") {
		t.Errorf("a raw line break from the path reached the log: %q", logs)
	}
	// LogValue() escapes the line break as \n, and slog's text handler quotes that again, so the
	// log holds the escape doubled: a backslash, then n.
	if !strings.Contains(logs, `\\nFAKE-LOG-LINE`) {
		t.Errorf("the path is not quoted in the log: %q", logs)
	}

	// The cap: a path of 400 bytes is logged as its first logCap bytes and "...". The path is
	// "/ui/" then z's, so the log keeps logCap minus four z's, then the mark.
	long := "/ui/" + strings.Repeat("z", 400)
	rec = newRecorder()
	chain.ServeHTTP(rec, hr.request(http.MethodPost, long, "csrf="+csrfFor(cookie), cookie,
		map[string]string{"Origin": "http://evil.test"}))
	logs = hr.logText()
	kept := logCap - len("/ui/")
	if !strings.Contains(logs, strings.Repeat("z", kept)+"...") {
		t.Errorf("the long path is not cut to %d bytes and marked: log %q", logCap, logs)
	}
	if strings.Contains(logs, strings.Repeat("z", kept+1)) {
		t.Errorf("the long path reached the log past its cap: log holds more than %d z's", kept)
	}
}

// formCase runs one POST through the form chain with the given headers, body and cookie,
// and returns the recorder and whether the action ran.
func formCase(hr *harness, cookie *http.Cookie, hdr map[string]string, body string) (*recorder, bool) {
	ran := false
	chain := hr.u.form(func(w http.ResponseWriter, _ *http.Request, _ signedIn) {
		ran = true
		w.WriteHeader(http.StatusNoContent)
	})
	rec := newRecorder()
	chain.ServeHTTP(rec, hr.request(http.MethodPost, "/ui/x", body, cookie, hdr))
	return rec, ran
}

// TestSameOriginFormWithItsRealOriginIsAccepted checks the required case: a same-origin form
// POST with its real Origin, and the right CSRF value, passes for an operator.
func TestSameOriginFormWithItsRealOriginIsAccepted(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	rec, ran := formCase(hr, cookie, map[string]string{"Origin": hr.origin()}, "csrf="+csrfFor(cookie))
	if !ran || rec.status() != http.StatusNoContent {
		t.Errorf("a same-origin form: ran %v status %d, want the action to run", ran, rec.status())
	}
}

// TestNullOriginIsRefused checks that an Origin of null is refused, even with a valid CSRF
// value, and that the refusal is the form message.
func TestNullOriginIsRefused(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	rec, ran := formCase(hr, cookie, map[string]string{"Origin": "null"}, "csrf="+csrfFor(cookie))
	if ran || rec.status() != http.StatusForbidden || !strings.Contains(rec.body.String(), msgFormRefused) {
		t.Errorf("Origin null: ran %v status %d body %q, want a 403 form refusal", ran, rec.status(), rec.body.String())
	}
	// The mismatch rule also refuses "null", so the status alone cannot show this clause
	// ran. The logged reason names it.
	if !strings.Contains(hr.logText(), "Origin is null") {
		t.Errorf("the refusal was not the null clause: log %q", hr.logText())
	}
}

// TestCrossOriginPOSTIsRefused checks that a POST from another origin is refused.
func TestCrossOriginPOSTIsRefused(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	for _, origin := range []string{"http://evil.test", "https://" + host, "http://" + host + ":8788"} {
		rec, ran := formCase(hr, cookie, map[string]string{"Origin": origin}, "csrf="+csrfFor(cookie))
		if ran || rec.status() != http.StatusForbidden {
			t.Errorf("Origin %q: ran %v status %d, want a 403 refusal", origin, ran, rec.status())
		}
	}
}

// TestOriginHeadersWithoutOriginFollowSecFetchSite checks the fallback: with no Origin, a
// Sec-Fetch-Site of same-origin is accepted, and any other value or none is refused.
func TestOriginHeadersWithoutOriginFollowSecFetchSite(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	cases := []struct {
		label string
		hdr   map[string]string
		ok    bool
	}{
		{"same-origin", map[string]string{"Sec-Fetch-Site": "same-origin"}, true},
		{"cross-site", map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{"none", map[string]string{"Sec-Fetch-Site": "none"}, false},
		{"neither header", nil, false},
	}
	for _, c := range cases {
		rec, ran := formCase(hr, cookie, c.hdr, "csrf="+csrfFor(cookie))
		if ran != c.ok {
			t.Errorf("%s: ran %v (status %d), want %v", c.label, ran, rec.status(), c.ok)
		}
	}
}

// TestTwoOriginHeadersAreRefused checks that a request with two Origin headers is refused,
// since no one value can be trusted.
func TestTwoOriginHeadersAreRefused(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	r := hr.request(http.MethodPost, "/ui/x", "csrf="+csrfFor(cookie), cookie, nil)
	r.Header.Add("Origin", hr.origin())
	r.Header.Add("Origin", hr.origin())
	ran := false
	rec := newRecorder()
	hr.u.form(func(http.ResponseWriter, *http.Request, signedIn) { ran = true }).ServeHTTP(rec, r)
	if ran || rec.status() != http.StatusForbidden {
		t.Errorf("two Origin headers: ran %v status %d, want a refusal", ran, rec.status())
	}
}

// TestCSRFMustMatchTheSession checks the CSRF rule: a missing field, a value from another
// session, and the HMAC of another label are each refused.
func TestCSRFMustMatchTheSession(t *testing.T) {
	hr := newHarness(t, false, false)
	mine := hr.signIn(hr.opKey, nil)
	theirs := hr.signIn(hr.viewKey, nil)
	otherLabel := encodeHMAC(mine.Value, "cairn-ui-other")
	cases := []struct{ label, body string }{
		{"no field", ""},
		{"empty field", "csrf="},
		{"another session's value", "csrf=" + csrfFor(theirs)},
		{"the HMAC of another label", "csrf=" + otherLabel},
		{"the raw token", "csrf=" + mine.Value},
	}
	for _, c := range cases {
		rec, ran := formCase(hr, mine, map[string]string{"Origin": hr.origin()}, c.body)
		if ran || rec.status() != http.StatusForbidden || !strings.Contains(rec.body.String(), msgFormRefused) {
			t.Errorf("%s: ran %v status %d, want a 403 CSRF refusal", c.label, ran, rec.status())
		}
	}
	if rec := hr.do(http.MethodGet, "/ui/", "", mine, nil); rec.status() != http.StatusOK {
		t.Errorf("a CSRF refusal ended the session: dashboard status %d", rec.status())
	}
}

// TestViewerPOSTIsRefusedBeforeTheBodyIsRead checks the role step: a viewer's POST is 403
// read_only, and the body is never read. The body is a trap that fails the test on a read.
func TestViewerPOSTIsRefusedBeforeTheBodyIsRead(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.viewKey, nil)
	trap := &bodyTrap{t: t}
	r := hr.request(http.MethodPost, "/ui/x", "", cookie, map[string]string{"Origin": hr.origin()})
	// The form content type is what makes ParseForm read the body, so the trap would fire
	// if the role check ran after the body read.
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Body = trap
	r.ContentLength = -1
	rec := newRecorder()
	hr.u.form(func(http.ResponseWriter, *http.Request, signedIn) {
		t.Error("the action ran for a viewer")
	}).ServeHTTP(rec, r)
	if rec.status() != http.StatusForbidden || !strings.Contains(rec.body.String(), msgReadOnly) {
		t.Errorf("viewer POST: status %d body %q, want 403 read-only", rec.status(), rec.body.String())
	}
	if trap.read {
		t.Error("the viewer's body was read")
	}
}

// TestViewerOriginRefusalComesBeforeRole checks the order: a viewer's POST with a bad origin
// is refused as an origin failure, not as read-only, since the origin is step two.
func TestViewerOriginRefusalComesBeforeRole(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.viewKey, nil)
	rec, _ := formCase(hr, cookie, map[string]string{"Origin": "http://evil.test"}, "")
	if !strings.Contains(rec.body.String(), msgFormRefused) || strings.Contains(rec.body.String(), msgReadOnly) {
		t.Errorf("viewer with a cross-origin POST: body %q, want the origin refusal", rec.body.String())
	}
}

// TestBodyOverTheCapIs413 checks the body cap of a form: one byte over 64 KiB is 413, and the
// action does not run. The sign-in body is capped at 4 KiB, and is refused the same way.
func TestBodyOverTheCapIs413(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	big := "csrf=" + csrfFor(cookie) + "&pad=" + strings.Repeat("x", formBodyCap)
	rec, ran := formCase(hr, cookie, map[string]string{"Origin": hr.origin()}, big)
	if ran || rec.status() != http.StatusRequestEntityTooLarge {
		t.Errorf("a form over 64 KiB: ran %v status %d, want 413", ran, rec.status())
	}
	rec = hr.do(http.MethodPost, "/ui/signin", "key="+strings.Repeat("x", signinBodyCap), nil, hr.signInHeaders())
	if rec.status() != http.StatusRequestEntityTooLarge {
		t.Errorf("a sign-in over 4 KiB: status %d, want 413", rec.status())
	}
}

// TestRefusedOriginIsLoggedQuotedAndCapped checks the log line of a refused origin: the Host
// and the Origin are quoted and cut short, so a line break or a long value cannot reach the
// log as it is, and the page never shows them.
func TestRefusedOriginIsLoggedQuotedAndCapped(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	evil := "http://evil.test/\nFAKE-LOG-LINE " + strings.Repeat("y", 400)
	rec, _ := formCase(hr, cookie, map[string]string{"Origin": evil}, "csrf="+csrfFor(cookie))
	if strings.Contains(rec.body.String(), "FAKE-LOG-LINE") {
		t.Error("the page shows the Origin")
	}
	logs := hr.logText()
	if strings.Contains(logs, "\nFAKE-LOG-LINE") {
		t.Errorf("a raw line break from the Origin reached the log: %q", logs)
	}
	if !strings.Contains(logs, `\n`) || !strings.Contains(logs, "...") {
		t.Errorf("the Origin is not quoted and capped in the log: %q", logs)
	}
	if strings.Count(logs, "y") > logCap+10 {
		t.Errorf("the Origin is not capped: the log holds %d y's", strings.Count(logs, "y"))
	}
}

// TestPagesEscapeAStoredName checks escaping: a key name that holds markup is shown as text.
func TestPagesEscapeAStoredName(t *testing.T) {
	hr := newHarness(t, false, false)
	hr.store.keys[hr.opID].name = `<script>alert(1)</script>`
	cookie := hr.signIn(hr.opKey, nil)
	body := hr.do(http.MethodGet, "/ui/", "", cookie, nil).body.String()
	if strings.Contains(body, "<script>") {
		t.Errorf("a stored name reached the page as markup: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("the stored name is not shown escaped: %s", body)
	}
}

// TestCSRFValueIsTheHMACOfTheSessionToken checks the derivation: the value is the same for
// one token, differs between tokens, and is 43 characters of unpadded base64url.
func TestCSRFValueIsTheHMACOfTheSessionToken(t *testing.T) {
	a, err := auth.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, err := auth.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if csrfToken(a) != csrfToken(a) || csrfToken(a) == csrfToken(b) {
		t.Error("the CSRF value is not a function of the token")
	}
	if got := csrfToken(a); len(got) != 43 || strings.ContainsAny(got, "=+/") {
		t.Errorf("CSRF value %q is not 43 characters of unpadded base64url", got)
	}
}

// TestSignInFormUsesTheSameOriginCheck checks that the sign-in POST refuses a null origin
// and a cross-origin post, before any key is looked at.
func TestSignInFormUsesTheSameOriginCheck(t *testing.T) {
	hr := newHarness(t, false, false)
	for _, origin := range []string{"null", "http://evil.test"} {
		rec := hr.do(http.MethodPost, "/ui/signin", "key="+hr.opKey, nil, map[string]string{"Origin": origin})
		if rec.status() != http.StatusForbidden || len(hr.store.sessions) != 0 {
			t.Errorf("sign-in from Origin %q: status %d, sessions %d; want a refusal and no session",
				origin, rec.status(), len(hr.store.sessions))
		}
	}
}

// TestSessionsAreCleanedUpOnSignIn checks that a sign-in removes an expired session of
// another browser, so the table does not grow without bound.
func TestSessionsAreCleanedUpOnSignIn(t *testing.T) {
	hr := newHarness(t, false, false)
	hr.signIn(hr.opKey, nil)
	hr.clock.t = hr.clock.t.Add(auth.SessionIdle + time.Minute)
	hr.signIn(hr.viewKey, nil)
	if len(hr.store.sessions) != 1 {
		t.Errorf("session rows after the idle one expired = %d, want 1", len(hr.store.sessions))
	}
}

// TestLastSeenIsNotWrittenOnEveryRequest checks the stored-activity rule through the UI: two
// requests in one minute move last_seen_at once.
func TestLastSeenIsNotWrittenOnEveryRequest(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	start := hr.clock.t
	hr.clock.t = start.Add(2 * time.Minute)
	hr.do(http.MethodGet, "/ui/", "", cookie, nil)
	first := hr.store.sessions[auth.HashToken(cookie.Value)].LastSeenAt
	hr.clock.t = start.Add(2*time.Minute + 10*time.Second)
	hr.do(http.MethodGet, "/ui/", "", cookie, nil)
	if got := hr.store.sessions[auth.HashToken(cookie.Value)].LastSeenAt; !got.Equal(first) {
		t.Errorf("last_seen_at moved within a minute: %v, was %v", got, first)
	}
}

// TestNoFailureIsRecordedOnAHappyPath is a guard on the harness: a normal session records
// no failure, so the failure hook is seen only when something is wrong.
func TestNoFailureIsRecordedOnAHappyPath(t *testing.T) {
	hr := newHarness(t, false, false)
	cookie := hr.signIn(hr.opKey, nil)
	hr.do(http.MethodGet, "/ui/", "", cookie, nil)
	if len(hr.failures) != 0 {
		t.Errorf("failures recorded on a happy path: %v", hr.failures)
	}
}
