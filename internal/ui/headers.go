package ui

import "net/http"

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
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		if hsts {
			h.Set("Strict-Transport-Security", hstsValue)
		}
		next.ServeHTTP(w, r)
	})
}
