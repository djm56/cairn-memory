package ui

import (
	"net/http"
	"path"
	"strings"
)

// contentSecurityPolicy is the policy of every answer under /ui/ (DESIGN §5.5). With no
// script-src, a page cannot run script; with style-src 'self', it cannot carry an inline
// style, so styling comes from app.css only.
const contentSecurityPolicy = "default-src 'none'; style-src 'self'; img-src 'self'; " +
	"form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

// hstsValue is the Strict-Transport-Security the server sends when HSTS is on. It has no
// includeSubDomains and no preload: the header is scoped to the host name, so the one
// year covers this host only, on every port (DESIGN §5.5, DEPLOY.md).
const hstsValue = "max-age=31536000"

// secureHeaders sets the security headers on every answer it wraps, before the wrapped
// handler runs, so the mux's own 404, 405 and redirect answers carry them too. hsts says
// whether to send Strict-Transport-Security.
func secureHeaders(hsts bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecureHeaders(w.Header(), hsts)
		next.ServeHTTP(w, r)
	})
}

// setSecureHeaders sets the security headers of DESIGN §5.5 on h.
func setSecureHeaders(h http.Header, hsts bool) {
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Cache-Control", "no-store")
	if hsts {
		h.Set("Strict-Transport-Security", hstsValue)
	}
}

// CoverPaths wraps next, the server's mux, so that every answer for a path under /ui
// carries the headers, including the redirect the mux makes for an unclean path before it
// routes anything: //ui/x, /ui/./x and /ui//x (DESIGN §5.5, Routes). The path is
// cleaned the way the mux cleans it, so the test here is the test the mux's redirect uses.
// hsts is as Config.HSTS.
func CoverPaths(hsts bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if underUI(r.URL.Path) {
			setSecureHeaders(w.Header(), hsts)
		}
		next.ServeHTTP(w, r)
	})
}

// underUI reports whether p, cleaned as ServeMux cleans a path, is /ui or a path below it.
func underUI(p string) bool {
	c := path.Clean(p)
	return c == "/ui" || strings.HasPrefix(c, "/ui/")
}
