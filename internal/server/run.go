package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/djm56/cairn-memory/internal/store"
)

// Options is what cairn serve starts with. Addr and DB are required. TLSCert and TLSKey
// are both set or both empty. BehindProxy says a proxy in front of the server terminates
// TLS, which the bind rule accepts in place of certificate files (D20).
type Options struct {
	Addr        string    // host:port to listen on
	DB          string    // the SQLite file the server opens; it is created when absent
	TLSCert     string    // PEM certificate file, or empty for no TLS
	TLSKey      string    // PEM key file, or empty for no TLS
	BehindProxy bool      // a proxy in front terminates TLS
	Log         io.Writer // where log lines go; nil means stderr
}

// Run serves the API until ctx is done or a SIGINT or SIGTERM arrives, then shuts down
// within shutdownTimeout. It refuses what the bind rules forbid before it opens the
// database, and a non-loopback bind with no active key before it listens. Its log lines
// never carry a header, a body, or a key.
func Run(ctx context.Context, o Options) error {
	if o.Log == nil {
		o.Log = os.Stderr
	}
	if err := checkOptions(o); err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(o.Addr)
	if err != nil {
		return fmt.Errorf("cairn serve: --addr %q: %w", o.Addr, err)
	}
	ips, err := ResolveHost(ctx, host)
	if err != nil {
		return fmt.Errorf("cairn serve: --addr %q: %w", o.Addr, err)
	}
	loopback := Loopback(ips)
	tlsOn := o.TLSCert != ""
	if err := RequireTransport(loopback, tlsOn, o.BehindProxy); err != nil {
		return fmt.Errorf("cairn serve: --addr %s: %w", o.Addr, err)
	}
	var tlsCfg *tls.Config
	if tlsOn {
		if tlsCfg, err = LoadTLS(o.TLSCert, o.TLSKey); err != nil {
			return fmt.Errorf("cairn serve: %w", err)
		}
	}

	st, err := openDatabase(o.DB, true)
	if err != nil {
		return fmt.Errorf("cairn serve: %w", err)
	}
	defer st.Close()
	keys, err := activeKeys(ctx, st)
	if err != nil {
		return fmt.Errorf("cairn serve: count the API keys: %w", err)
	}
	if err := RequireKeys(loopback, keys); err != nil {
		return fmt.Errorf("cairn serve: --addr %s: %w", o.Addr, err)
	}
	if loopback && keys == 0 {
		fmt.Fprintln(o.Log, NoKeysLine)
	}
	if line := CleartextLine(loopback, tlsOn, o.BehindProxy); line != "" {
		fmt.Fprintln(o.Log, line)
	}

	// A loopback host that resolved is listened on at the address that was checked, so
	// the bind is the one the rule saw. Any other host is listened on as given.
	listenAddr := o.Addr
	if loopback {
		listenAddr = net.JoinHostPort(ips[0].String(), port)
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("cairn serve: listen on %s: %w", listenAddr, err)
	}

	logger := slog.New(slog.NewTextHandler(o.Log, nil))
	logger.Info("listening", "addr", ln.Addr().String(), "db", o.DB,
		"tls", tlsOn, "behind_proxy", o.BehindProxy, "active_keys", keys)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Once the stop has begun, the signal handling is released, so a second Ctrl-C during
	// the drain ends the process at once instead of being swallowed until Run returns.
	go func() {
		<-ctx.Done()
		stop()
	}()
	if err := Serve(ctx, ln, Handler(st, logger), tlsCfg, logger); err != nil {
		return fmt.Errorf("cairn serve: %w", err)
	}
	logger.Info("stopped")
	return nil
}

// checkOptions refuses the option values that are wrong whatever the bind is. The
// command checks the same things with its own messages; Run checks them too, so a
// caller of Run cannot skip them.
func checkOptions(o Options) error {
	if o.Addr == "" {
		return errors.New("cairn serve: --addr is required")
	}
	if err := checkDBPath(o.DB); err != nil {
		return fmt.Errorf("cairn serve: %w", err)
	}
	if (o.TLSCert == "") != (o.TLSKey == "") {
		return errors.New("cairn serve: --tls-cert and --tls-key go together: give both, or neither")
	}
	return nil
}

// activeKeys counts the API keys that are not revoked.
func activeKeys(ctx context.Context, st *store.Store) (int, error) {
	n := 0
	err := st.WithReadTx(ctx, func(q store.Querier) error {
		keys, err := store.ListAPIKeys(ctx, q)
		if err != nil {
			return err
		}
		for _, k := range keys {
			if k.RevokedAt == nil {
				n++
			}
		}
		return nil
	})
	return n, err
}

// LoadTLS reads a PEM certificate and key and returns the TLS settings of the server,
// at TLS 1.2 or later (D20). Its error names the failure, not the key's contents.
func LoadTLS(certFile, keyFile string) (*tls.Config, error) {
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load the TLS certificate: %w", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}, nil
}

// Serve serves h on ln until ctx is done, then shuts down. In-flight calls get
// shutdownTimeout to finish; any still running after it are closed. tlsCfg, when set,
// wraps ln in TLS at no less than TLS 1.2. The timeouts and the header cap are those of
// DESIGN §5.3. The server's own log lines go to logger.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, tlsCfg *tls.Config, logger *slog.Logger) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	if tlsCfg != nil {
		cfg := tlsCfg.Clone()
		if cfg.MinVersion < tls.VersionTLS12 {
			cfg.MinVersion = tls.VersionTLS12
		}
		ln = tls.NewListener(ln, cfg)
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
