package ui

import (
	"errors"
	"net/http"
	"strings"

	"github.com/djm56/cairn-memory/internal/auth"
)

// The sign-in messages. They are fixed text: none repeats what was typed, and none names
// the key (DESIGN §5.5).
const (
	msgAgentKey   = "This is an agent key. The web UI signs in operator and viewer keys."
	msgKeyRefused = "The key was not accepted."
)

// signinPage shows the sign-in form. A browser that already holds a live session is sent
// to the dashboard instead. A failure of the store is answered as internal.
func (u *ui) signinPage(w http.ResponseWriter, r *http.Request) {
	if token := u.presented(r); token != "" {
		_, err := u.cfg.Sessions.Check(r.Context(), token)
		switch {
		case err == nil:
			http.Redirect(w, r, pathRoot, http.StatusSeeOther)
			return
		case !isNoSession(err):
			u.internal(w, r, err)
			return
		}
	}
	u.render(w, http.StatusOK, "signin", view{Title: "Sign in"})
}

// signinPost signs a key in. Its checks run in this order: the origin (there is no
// session yet, so no session check and no CSRF value), then the body, capped at 4 KiB.
// The key is trimmed of white space at each end and checked by auth. An operator or
// viewer key makes a session, the browser's previous session is deleted, the cookie is
// set, and the browser is sent to the dashboard. Any other key re-renders the form with
// the refusal, at 401, and leaves the browser's session as it was.
func (u *ui) signinPost(w http.ResponseWriter, r *http.Request) {
	if !u.originOK(r) {
		u.fail(w, http.StatusForbidden, msgFormRefused)
		return
	}
	if !u.readForm(w, r, signinBodyCap) {
		return
	}
	key := strings.TrimSpace(r.PostForm.Get("key"))
	_, token, err := u.cfg.Sessions.SignIn(r.Context(), key, u.presented(r))
	switch {
	case err == nil:
		u.setCookie(w, token)
		http.Redirect(w, r, pathRoot, http.StatusSeeOther)
	case errors.Is(err, auth.ErrAgentKey):
		u.signinRefused(w, msgAgentKey)
	case errors.Is(err, auth.ErrUnauthenticated):
		u.signinRefused(w, msgKeyRefused)
	default:
		u.internal(w, r, err)
	}
}

// signinRefused re-renders the sign-in form with a refusal, at 401. The form is empty:
// the key is never shown back.
func (u *ui) signinRefused(w http.ResponseWriter, msg string) {
	u.render(w, http.StatusUnauthorized, "signin", view{Title: "Sign in", Message: msg})
}
