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

// Handler returns the server's handler over st: the calls under /v1, the liveness
// probe at /healthz, and the body cap over both. The causes of failures and the
// failures of last_used_at go to logger.
func Handler(st *store.Store, logger *slog.Logger) http.Handler {
	return handler(api.NewStoreLookup(st), core.NewLocal(st), logger)
}

// handler builds the server's handler over a key lookup and a Service. Handler passes
// the store's; a test passes its own to reach a failure the store cannot give.
func handler(lookup auth.Lookup, svc core.Service, logger *slog.Logger) http.Handler {
	resolver := auth.NewResolver(lookup, func(err error) {
		// A failed last_used_at write does not refuse the call that used the key, but it
		// is logged, never dropped (DESIGN §5.3).
		logger.Error("last_used_at was not written", "cause", redact(err.Error()))
	})
	calls := api.Handler(svc, api.KeyCaller(resolver), failureHook(logger))
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthz)
	mux.Handle("/", calls)
	return http.MaxBytesHandler(mux, api.MaxBodyBytes+bodyAllowance)
}

// failureHook logs the cause of each internal failure (CONTRACT §1.2, DESIGN §5.4,
// issue #112). It logs the method, the route path and the cause, redacted. It takes
// no header and no body, so the Authorization header, and the key in it, cannot reach
// the log through this hook.
func failureHook(logger *slog.Logger) api.FailureHook {
	return func(r *http.Request, err error) {
		logger.Error("internal failure", "method", r.Method, "path", r.URL.Path, "cause", redact(err.Error()))
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
