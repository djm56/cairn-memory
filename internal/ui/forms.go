package ui

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/djm56/cairn-memory/internal/auth"
)

// The messages a form answer shows. They are fixed text: none carries a stored value, a
// header, or what the operator typed (DESIGN §5.5).
const (
	msgFormRefused = "The form could not be verified. Load the page again and try again."
	msgReadOnly    = "This key is read-only."
	msgTooLarge    = "The form is too large."
	msgUnreadable  = "The form could not be read."
)

// csrfLabel is the fixed message the CSRF value is the HMAC of. It is a label, not a
// secret: it keeps this value apart from any other HMAC of the same token.
const csrfLabel = "cairn-ui-csrf"

// logCap is the longest a header value is written to the log. A Host or Origin is
// attacker-controlled text, so it is quoted and cut short before it is logged.
const logCap = 200

// scheme is the scheme a same-origin form carries: https when the server serves TLS or
// runs behind a proxy that terminates it, which is what Secure means, and http otherwise.
func (u *ui) scheme() string {
	if u.cfg.Secure {
		return "https"
	}
	return "http"
}

// originOK reports whether the request's Origin says it came from this server (DESIGN
// §5.5, step 2). A present Origin must equal the scheme and Host of the request, and the
// value null is refused, since a same-origin form under Referrer-Policy same-origin never
// sends it. An absent Origin is accepted only when Sec-Fetch-Site says same-origin. A
// request with neither, or with two Origin headers, is refused. A refusal is logged with
// the Host and Origin quoted and cut short, and the browser sees only the form message.
func (u *ui) originOK(r *http.Request) bool {
	values := r.Header.Values("Origin")
	if len(values) == 0 {
		if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
			return true
		}
		u.refusedOrigin(r, "", "no Origin, and no same-origin Sec-Fetch-Site")
		return false
	}
	origin := values[0]
	switch {
	case len(values) > 1:
		u.refusedOrigin(r, origin, "more than one Origin header")
		return false
	case origin == "null":
		u.refusedOrigin(r, origin, "Origin is null")
		return false
	case !strings.EqualFold(origin, u.scheme()+"://"+r.Host):
		u.refusedOrigin(r, origin, "Origin does not match the request's scheme and Host")
		return false
	}
	return true
}

// refusedOrigin logs one refused origin check. The values are quoted and cut to logCap,
// so a hostile header cannot put a line break or a long string into the log.
func (u *ui) refusedOrigin(r *http.Request, origin, reason string) {
	u.log.Warn("form refused", "reason", reason, "path", r.URL.Path,
		"host", quoted(r.Host), "origin", quoted(origin))
}

// quoted returns s as a quoted Go string, cut to logCap bytes first. A cut is marked with
// "...", so the log shows that the value was longer.
func quoted(s string) string {
	if len(s) > logCap {
		s = s[:logCap] + "..."
	}
	return strconv.Quote(s)
}

// csrfToken returns the CSRF value of the session whose cookie token is token: HMAC-SHA256
// keyed by the token, over csrfLabel, in unpadded base64url. Nothing is stored, so the
// value is recomputed on each request and needs no column (DESIGN §5.5).
func csrfToken(token string) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte(csrfLabel))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// csrfOK reports whether got is the CSRF value of token. The comparison is constant
// time (hmac.Equal), so a guess is not timed.
func csrfOK(token, got string) bool {
	return hmac.Equal([]byte(got), []byte(csrfToken(token)))
}

// readForm reads and parses the form body, capped at limit bytes. A larger body is 413
// and a body that does not parse is 400: each is answered here, and readForm returns
// false. A body that parses is in r.PostForm.
func (u *ui) readForm(w http.ResponseWriter, r *http.Request, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseForm(); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			u.fail(w, http.StatusRequestEntityTooLarge, msgTooLarge)
			return false
		}
		u.fail(w, http.StatusBadRequest, msgUnreadable)
		return false
	}
	return true
}

// form wraps a POST that writes. It checks, in this order, and h runs only when every
// check passes (DESIGN §5.5):
//
//  1. the session: none is a redirect to sign-in, and nothing is done;
//  2. the origin: a refusal is 403 with the form message;
//  3. the role: a viewer is 403 read_only, answered before the body is read;
//  4. the body, capped at 64 KiB: a larger one is 413;
//  5. the csrf field, compared with the session's CSRF value: a mismatch is 403.
//
// A refused check does not end the session.
func (u *ui) form(h func(http.ResponseWriter, *http.Request, signedIn)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := u.session(w, r)
		if !ok {
			return
		}
		if !u.originOK(r) {
			u.fail(w, http.StatusForbidden, msgFormRefused)
			return
		}
		if s.Key.Scope == auth.ScopeViewer {
			u.fail(w, http.StatusForbidden, msgReadOnly)
			return
		}
		if !u.readForm(w, r, formBodyCap) {
			return
		}
		if !csrfOK(s.token, r.PostForm.Get("csrf")) {
			u.fail(w, http.StatusForbidden, msgFormRefused)
			return
		}
		h(w, r, s)
	}
}
