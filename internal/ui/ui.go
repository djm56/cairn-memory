// Package ui is the web UI of cairn serve: pages rendered on the server, under /ui/
// (docs/DESIGN.md §5.5, decisions D23 to D26). It needs no script and loads one
// embedded stylesheet. Sign-in, the session rules and their lifetimes are in
// internal/auth; this package holds the routes, the forms, the headers and the pages.
//
// The package imports no store and no server (the reach guard, internal/core, holds it
// to that). It reaches the session table through auth.Sessions, which the server builds
// over the store. The pages that call core.Service arrive in later steps.
package ui

import (
	"bytes"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/djm56/cairn-memory/internal/auth"
)

// Config is what the UI is built over.
type Config struct {
	// Sessions makes, checks and ends the sessions of the browsers.
	Sessions *auth.Sessions
	// Secure is set when the server serves TLS itself or runs behind a proxy that
	// terminates it (DESIGN §5.5). It sets the cookie's Secure flag, its __Secure- name,
	// and the https scheme a form's Origin must carry.
	Secure bool
	// HSTS is set when the server serves TLS on a non-loopback address. The server
	// sends Strict-Transport-Security only then (DESIGN §5.5, DEPLOY.md).
	HSTS bool
	// Logger receives the server's log lines about the UI: refused forms, and failures
	// the browser sees only as "internal error". Nil discards them.
	Logger *slog.Logger
	// Redact removes any key from a cause before it is logged. Nil keeps the cause as it
	// is, which is correct only when no cause can carry a key.
	Redact func(string) string
}

// Paths the UI answers, as DESIGN §5.5 lists them.
const (
	pathRoot    = "/ui/"
	pathSignin  = "/ui/signin"
	pathSignout = "/ui/signout"
	pathStyles  = "/ui/static/app.css"
)

// The body caps (DESIGN §5.5): a sign-in form is 4 KiB, any other form 64 KiB.
const (
	signinBodyCap = 4 << 10
	formBodyCap   = 64 << 10
)

// ui is the UI over one Config. It holds no request state, so its handlers are safe for
// concurrent use.
type ui struct {
	cfg Config
	log *slog.Logger
}

// New returns the UI's handler: every route under /ui/, wrapped in the security headers.
// The server mounts it at both /ui and /ui/, so the redirect from /ui carries the headers
// too. A path the UI does not list is 404, and a wrong method is 405 with Allow.
func New(cfg Config) http.Handler {
	return newUI(cfg).handler()
}

// newUI builds the UI over cfg, with a discarding logger when none is given.
func newUI(cfg Config) *ui {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &ui{cfg: cfg, log: logger}
}

// handler builds the route table of DESIGN §5.5.
func (u *ui) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui", redirectToRoot)
	mux.HandleFunc("GET /ui/{$}", u.page(u.dashboard))
	mux.HandleFunc("GET "+pathSignin, u.signinPage)
	mux.HandleFunc("POST "+pathSignin, u.signinPost)
	mux.HandleFunc("GET "+pathSignout, u.signout)
	mux.HandleFunc("GET "+pathStyles, serveStyles)
	return secureHeaders(u.cfg.HSTS, mux)
}

// redirectToRoot sends /ui to the dashboard, with its trailing slash. It is a permanent
// redirect, as the mux gives for a path that needs cleaning.
func redirectToRoot(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, pathRoot, http.StatusMovedPermanently)
}

// serveStyles answers the embedded stylesheet. It needs no session.
func serveStyles(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write(stylesheet)
}

// internal answers a failure whose cause the browser must not see. The page says
// "internal error", and the cause goes to the log, redacted.
func (u *ui) internal(w http.ResponseWriter, r *http.Request, err error) {
	cause := err.Error()
	if u.cfg.Redact != nil {
		cause = u.cfg.Redact(cause)
	}
	u.log.Error("internal failure", "method", r.Method, "path", r.URL.Path, "cause", cause)
	u.fail(w, http.StatusInternalServerError, "internal error")
}

// fail answers status with the error page, showing msg as text. msg is always one of the
// fixed messages of DESIGN §5.5, never a stored value.
func (u *ui) fail(w http.ResponseWriter, status int, msg string) {
	u.render(w, status, "error", view{Title: http.StatusText(status), Message: msg})
}

// render writes the named page with status. The page is built in a buffer first, so a
// template that fails is seen before any byte of it is sent, and the answer is a bare 500.
func (u *ui) render(w http.ResponseWriter, status int, name string, v view) {
	t, ok := pages[name]
	if !ok {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", v); err != nil {
		u.log.Error("render failed", "page", name, "cause", err.Error())
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// view is the data every page is rendered with. A page reads the fields it needs. Name
// is a stored value, so the template shows it as text.
type view struct {
	Title   string
	Message string
	Name    string // the signed-in key's name
	Role    string // the signed-in key's role: operator or viewer
}

// pages holds one parsed template set per page. Each set is the layout and one page, so
// the "title" and "body" blocks of one page cannot clash with another's.
var pages = map[string]*template.Template{
	"signin":    parsePage("signin"),
	"dashboard": parsePage("dashboard"),
	"error":     parsePage("error"),
}

// parsePage parses the layout and the named page. A template that does not parse is a
// build fault, so it stops the program at start rather than at the first request.
func parsePage(name string) *template.Template {
	return template.Must(template.ParseFS(templateFiles, "templates/layout.html", "templates/"+name+".html"))
}
