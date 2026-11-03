package server

import (
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
	"github.com/djm56/cairn-memory/internal/ui"
)

// The limits and timeouts of the listener (DESIGN §5.3, D20). They are fixed rather than
// flags, so every server runs with the same bounds.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 120 * time.Second
	maxHeaderBytes    = 64 << 10
	// bodyAllowance is added to the handler's body cap for the whole server. The
	// handler's own 413 is the answer a client sees for a body just over 1 MiB, and this
	// outer cap stops a body that is far larger before it is read at all.
	bodyAllowance = 64 << 10
	// shutdownTimeout bounds how long in-flight calls may run after a stop signal.
	shutdownTimeout = 10 * time.Second
)

// Transport is how the server is reached. The web UI needs it for its cookie's Secure
// flag, the https scheme of a form's Origin, and HSTS (DESIGN §5.5). Run sets it from the
// flags and the listen address.
type Transport struct {
	TLS         bool // the server serves TLS itself (--tls-cert and --tls-key)
	BehindProxy bool // a proxy in front terminates TLS (--behind-proxy)
	Loopback    bool // the listen address is a loopback address
}

// Handler returns the server's handler over st, for tests only (see NewHandler). It takes
// the zero Transport, so the UI's cookie is not Secure and HSTS is not sent: wrong for any
// server reached over TLS or a proxy. A served server uses NewHandler with its real
// Transport, as Run does. The handler holds the calls under /v1, the liveness probe at
// /healthz, the web UI under /ui, and the body cap over all of them. The causes of failures
// and the failures of last_used_at go to logger.
func Handler(st *store.Store, logger *slog.Logger) http.Handler {
	return NewHandler(st, logger, Transport{})
}

// NewHandler is Handler with the transport the server is reached over.
func NewHandler(st *store.Store, logger *slog.Logger, tr Transport) http.Handler {
	return handler(api.NewStoreLookup(st), NewSessionStore(st), core.NewLocal(st), logger, tr)
}

// handler builds the server's handler over a key lookup, a session store and a Service.
// NewHandler passes the store's; a test passes its own to reach a failure the store
// cannot give.
func handler(lookup auth.Lookup, sessions auth.SessionStore, svc core.Service, logger *slog.Logger, tr Transport) http.Handler {
	resolver := auth.NewResolver(lookup, func(err error) {
		// A failed last_used_at write does not refuse the call that used the key, but it
		// is logged, never dropped (DESIGN §5.3).
		logger.Error("last_used_at was not written", "cause", redact(err.Error()))
	})
	calls := api.Handler(svc, api.KeyCaller(resolver), failureHook(logger))
	sess := auth.NewSessions(sessions, resolver, nil, func(err error) {
		// A session failure that refuses nothing (a last_seen_at write, a cleanup) is logged
		// with the same redaction as every other cause.
		logger.Error("web UI session failure", "cause", redact(err.Error()))
	})
	// HSTS is scoped to the host name, not the port, so a trusted certificate here would
	// hold every other service on this host to https. Loopback is therefore excluded.
	hsts := tr.TLS && !tr.Loopback
	web := ui.New(ui.Config{
		Sessions: sess,
		Service:  svc,
		Secure:   tr.TLS || tr.BehindProxy,
		HSTS:     hsts,
		Logger:   logger,
		Redact:   redact,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	// /ui and /ui/ both go to the UI, so the redirect from /ui carries its headers and
	// the mux does not answer it itself.
	mux.Handle("/ui", web)
	mux.Handle("/ui/", web)
	mux.Handle("/", calls)
	// The mux answers an unclean path such as //ui/x with its own redirect, before it routes
	// anything, so CoverPaths sets the UI's headers outside the mux for that answer too.
	return http.MaxBytesHandler(ui.CoverPaths(hsts, mux), api.MaxBodyBytes+bodyAllowance)
}

// failureHook logs the cause of each internal failure (CONTRACT §1.2, DESIGN §5.4,
// issue #112). It logs the method, the route path and the cause, redacted. The path is
// quoted and cut to the UI's log cap (ui.LogValue, #160), so a path with a line break or
// a long length cannot forge or grow the line. It takes no header and no body, so the
// Authorization header, and the key in it, cannot reach the log through this hook.
func failureHook(logger *slog.Logger) api.FailureHook {
	return func(r *http.Request, err error) {
		logger.Error("internal failure", "method", r.Method, "path", ui.LogValue(r.URL.Path), "cause", redact(err.Error()))
	}
}

// healthz answers the liveness probe. It is GET only and needs no key. Its body is a
// fixed string that reveals no version and no configuration.
func healthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, "{\"status\":\"ok\"}\n")
}

// keyPattern matches an API key in text: the key prefix and the base64url characters
// after it. The count is not fixed at 43, so a key cut short, or one with characters
// added, is still removed: a fragment of a key in a cause is still a key in the log.
var keyPattern = regexp.MustCompile(regexp.QuoteMeta(auth.KeyPrefix) + `[A-Za-z0-9_-]+`)

// bearerPattern matches a bearer credential in text, in case one reaches a cause.
var bearerPattern = regexp.MustCompile(`(?i)bearer\s+\S+`)

// redact removes any API key, and any bearer credential, from text before it is
// logged. The hooks take no header, so this is a second guard: a cause whose text
// carries a key is still written without it.
func redact(text string) string {
	text = keyPattern.ReplaceAllString(text, auth.KeyPrefix+"<redacted>")
	return bearerPattern.ReplaceAllString(text, "Bearer <redacted>")
}
