package ui

import (
	"errors"
	"net/http"

	"github.com/djm56/cairn-memory/internal/auth"
)

// cookieBase is the cookie's name without its prefix. Over TLS, or behind a proxy that
// terminates it, the name is __Secure-cairn_ui, so a browser refuses the cookie unless it
// is marked Secure (DESIGN §5.5).
const cookieBase = "cairn_ui"

// signedIn is a request's live session: the session as Check reports it, and the token
// the cookie carried. The token keys the CSRF value of the session's forms.
type signedIn struct {
	auth.Session
	token string
}

// cookieName is the name of the session cookie for this server.
func (u *ui) cookieName() string {
	if u.cfg.Secure {
		return "__Secure-" + cookieBase
	}
	return cookieBase
}

// presented returns the token the browser sent in its cookie, or "" when it sent none.
func (u *ui) presented(r *http.Request) string {
	c, err := r.Cookie(u.cookieName())
	if err != nil {
		return ""
	}
	return c.Value
}

// setCookie sets the session cookie to token. It is a browser session cookie, with no
// Max-Age and no Expires: the server's lifetimes decide when a session ends. It is
// HttpOnly, SameSite=Strict and scoped to /ui.
func (u *ui) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, u.cookie(token))
}

// clearCookie sends the session cookie again with Max-Age=0. Its name, path and flags are
// the same as setCookie's, so the browser drops the one it holds.
func (u *ui) clearCookie(w http.ResponseWriter) {
	c := u.cookie("")
	c.MaxAge = -1
	http.SetCookie(w, c)
}

// cookie returns the session cookie with value.
func (u *ui) cookie(value string) *http.Cookie {
	return &http.Cookie{
		Name:     u.cookieName(),
		Value:    value,
		Path:     "/ui",
		HttpOnly: true,
		Secure:   u.cfg.Secure,
		SameSite: http.SameSiteStrictMode,
	}
}

// session checks the request's session. A browser with none, or whose session has ended,
// is redirected to sign-in and ok is false. A failure of the store is answered as
// internal, and ok is false too.
func (u *ui) session(w http.ResponseWriter, r *http.Request) (signedIn, bool) {
	token := u.presented(r)
	if token == "" {
		redirectToSignin(w, r)
		return signedIn{}, false
	}
	s, err := u.cfg.Sessions.Check(r.Context(), token)
	if isNoSession(err) {
		redirectToSignin(w, r)
		return signedIn{}, false
	}
	if err != nil {
		u.internal(w, r, err)
		return signedIn{}, false
	}
	return signedIn{Session: s, token: token}, true
}

// isNoSession reports whether err is the session check's answer for a browser with no
// live session.
func isNoSession(err error) bool {
	return errors.Is(err, auth.ErrNoSession)
}

// redirectToSignin sends the browser to the sign-in page with no next parameter, so a
// link cannot carry it to another address (DESIGN §5.5). It is 303, so a POST is not
// repeated at sign-in.
func redirectToSignin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, pathSignin, http.StatusSeeOther)
}

// page wraps a GET page that needs a live session. h runs only with one.
func (u *ui) page(h func(http.ResponseWriter, *http.Request, signedIn)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := u.session(w, r)
		if !ok {
			return
		}
		h(w, r, s)
	}
}

// signout ends the browser's session and clears its cookie, then sends the browser to
// sign-in (DESIGN §5.5). It needs no live session: a browser with none has nothing to
// end, and the cookie is cleared all the same. The cookie is cleared before the store is
// called, so a failed delete still leaves the browser signed out; the failure is logged
// and answered as internal.
func (u *ui) signout(w http.ResponseWriter, r *http.Request) {
	token := u.presented(r)
	u.clearCookie(w)
	if token != "" {
		if err := u.cfg.Sessions.End(r.Context(), token); err != nil {
			u.internal(w, r, err)
			return
		}
	}
	redirectToSignin(w, r)
}

// dashboard is the placeholder dashboard: who is signed in, and the sign-out link. The
// figures of CONTRACT §8.1 replace it in a later step.
func (u *ui) dashboard(w http.ResponseWriter, _ *http.Request, s signedIn) {
	u.render(w, http.StatusOK, "dashboard", view{
		Title: "Dashboard",
		Name:  s.Key.Name,
		Role:  string(s.Key.Scope),
	})
}
