package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// syncBuffer is a log sink. The server writes to it from its own goroutines, and the
// test reads it, so each access takes the lock.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// testLogger returns a text logger over w, as the server builds its own.
func testLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, nil))
}

// openStore opens a new database in the test's directory.
func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cairn.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// addKey stores an active key named name with scope, and returns its plaintext. The
// store keeps only the hash, as it does for a key an operator has just made.
func addKey(t *testing.T, st *store.Store, name string, scope auth.Scope) string {
	t.Helper()
	key, err := auth.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	if err := st.WithTx(context.Background(), func(q store.Querier) error {
		_, err := store.AddAPIKey(context.Background(), q, store.APIKeyNew{
			Name: name, Hash: auth.HashKey(key), Scope: string(scope), CreatedAt: time.Now(),
		})
		return err
	}); err != nil {
		t.Fatalf("add key %s: %v", name, err)
	}
	return key
}

// startServer serves h on a listener at 127.0.0.1:0, so the test never binds another
// address. It returns the base URL, with the scheme of tlsCfg, and registers the stop.
func startServer(t *testing.T, h http.Handler, tlsCfg *tls.Config) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- Serve(ctx, ln, h, tlsCfg, testLogger(io.Discard)) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errc:
			if err != nil {
				t.Errorf("Serve returned %v after the stop, want nil", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("Serve did not return within 15s of the stop")
		}
	})
	scheme := "http"
	if tlsCfg != nil {
		scheme = "https"
	}
	return scheme + "://" + ln.Addr().String()
}

// send makes one request and returns the answer and its body. A body is sent as JSON
// unless headers names a Content-Type.
func send(t *testing.T, c *http.Client, method, url, body string, headers map[string]string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read answer: %v", err)
	}
	return resp, out
}

// callHeaders returns the headers of an agent call from acme-api, with key as the bearer.
func callHeaders(key string) map[string]string {
	return map[string]string{
		"X-Cairn-Project": "acme-api",
		"X-Cairn-Agent":   "main",
		"Authorization":   "Bearer " + key,
	}
}

// client returns an HTTP client with a bound on each call.
func client(tr *http.Transport) *http.Client {
	c := &http.Client{Timeout: 15 * time.Second}
	if tr != nil {
		c.Transport = tr
	}
	return c
}

// TestHealthzNeedsNoKeyAndRefusesOtherMethods checks the probe: GET answers 200 with the
// fixed body and no key, and any other method is 405 with Allow: GET.
func TestHealthzNeedsNoKeyAndRefusesOtherMethods(t *testing.T) {
	st := openStore(t)
	base := startServer(t, Handler(st, testLogger(io.Discard)), nil)
	c := client(nil)

	resp, body := send(t, c, http.MethodGet, base+"/healthz", "", nil)
	if resp.StatusCode != http.StatusOK || string(body) != "{\"status\":\"ok\"}\n" {
		t.Errorf("GET /healthz = %d %q, want 200 with the fixed body", resp.StatusCode, body)
	}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		resp, _ := send(t, c, m, base+"/healthz", "", nil)
		if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != http.MethodGet {
			t.Errorf("%s /healthz = %d, Allow %q; want 405 with Allow GET", m, resp.StatusCode, resp.Header.Get("Allow"))
		}
	}
}

// TestKeyedCallWorksEndToEnd is the served path over a real listener: a key made in the
// store opens the calls, and a task started by one call is read back by the next.
func TestKeyedCallWorksEndToEnd(t *testing.T) {
	st := openStore(t)
	key := addKey(t, st, "ci", auth.ScopeAgent)
	base := startServer(t, Handler(st, testLogger(io.Discard)), nil)
	c := client(nil)

	resp, body := send(t, c, http.MethodPost, base+"/v1/agent/task_start",
		`{"title":"serve smoke","goal":"reach the server"}`, callHeaders(key))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("task_start = %d: %s", resp.StatusCode, body)
	}
	resp, body = send(t, c, http.MethodPost, base+"/v1/agent/task_current", "", callHeaders(key))
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "serve smoke") {
		t.Errorf("task_current = %d: %s; want the task just started", resp.StatusCode, body)
	}
}

// TestNoKeyIs401WithTheBearerChallenge checks a call with no key: 401, the kind
// unauthenticated, and WWW-Authenticate: Bearer (RFC 7235).
func TestNoKeyIs401WithTheBearerChallenge(t *testing.T) {
	st := openStore(t)
	base := startServer(t, Handler(st, testLogger(io.Discard)), nil)
	resp, body := send(t, client(nil), http.MethodPost, base+"/v1/agent/task_current", "",
		map[string]string{"X-Cairn-Project": "acme-api"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no key: status %d, want 401: %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
	if !strings.Contains(string(body), `"unauthenticated"`) {
		t.Errorf("body %s does not carry the kind unauthenticated", body)
	}
}

// TestOversizeBodyIs413 sends a keyed body of 2 MiB. The handler's cap answers 413 with
// the kind invalid, and the call does not panic: a panic would be a 500.
func TestOversizeBodyIs413(t *testing.T) {
	st := openStore(t)
	key := addKey(t, st, "ci", auth.ScopeAgent)
	base := startServer(t, Handler(st, testLogger(io.Discard)), nil)
	big := `{"title":"` + strings.Repeat("a", 2<<20) + `","goal":"g"}`
	resp, body := send(t, client(nil), http.MethodPost, base+"/v1/agent/task_start", big, callHeaders(key))
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("2 MiB body: status %d, want 413: %.200s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), `"invalid"`) {
		t.Errorf("413 body %s does not carry the kind invalid", body)
	}
}

// TestOversizeHeaderIs431 sends a request whose one header is 100 KiB, over the 64 KiB
// header cap. The server answers 431 and closes, and does not panic.
func TestOversizeHeaderIs431(t *testing.T) {
	st := openStore(t)
	base := startServer(t, Handler(st, testLogger(io.Discard)), nil)
	addr := strings.TrimPrefix(base, "http://")
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	req := "GET /healthz HTTP/1.1\r\nHost: " + addr + "\r\nX-Big: " + strings.Repeat("a", 100<<10) + "\r\n\r\n"
	// The write may fail once the server closes; the status line is what the test reads.
	go func() { _, _ = conn.Write([]byte(req)) }()
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read status line: %v", err)
	}
	if !strings.HasPrefix(line, "HTTP/1.1 431 ") {
		t.Errorf("100 KiB header: status line %q, want 431", strings.TrimSpace(line))
	}
}

// TestInternalFailureLogsTheCauseNotTheKey drives an internal failure with a bearer key
// in the request. The store is closed under the running server, so the key lookup fails
// with a cause that is not a core error. The failure hook must receive the cause and log
// it, and the log must not hold the key. The answer must not carry the cause either.
func TestInternalFailureLogsTheCauseNotTheKey(t *testing.T) {
	st := openStore(t)
	key := addKey(t, st, "ci", auth.ScopeAgent)
	var logs syncBuffer
	base := startServer(t, Handler(st, testLogger(&logs)), nil)
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	resp, body := send(t, client(nil), http.MethodPost, base+"/v1/agent/task_current", "", callHeaders(key))
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500: %s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "closed") {
		t.Errorf("the answer carries the cause: %s", body)
	}

	text := logs.String()
	if !strings.Contains(text, "internal failure") || !strings.Contains(text, "database is closed") {
		t.Errorf("the hook did not log the cause: %q", text)
	}
	if !strings.Contains(text, "path=/v1/agent/task_current") {
		t.Errorf("the hook did not log the route path: %q", text)
	}
	if strings.Contains(text, key) {
		t.Errorf("the log holds the API key: %q", text)
	}
	if strings.Contains(text, "Bearer") || strings.Contains(text, "Authorization") {
		t.Errorf("the log holds the Authorization header: %q", text)
	}
}

// touchFails is a key lookup that finds an active key, and whose last_used_at write fails.
type touchFails struct{}

func (touchFails) FindActive(context.Context, string) (auth.Key, error) {
	return auth.Key{ID: 1, Name: "ci", Scope: auth.ScopeAgent}, nil
}

func (touchFails) Touch(context.Context, int64, time.Time) error {
	return errors.New("touch broke")
}

// TestTouchFailureIsLoggedAndDoesNotRefuse checks that a failed last_used_at write is
// logged, and that the call which used the key still succeeds (DESIGN §8).
func TestTouchFailureIsLoggedAndDoesNotRefuse(t *testing.T) {
	st := openStore(t)
	var logs syncBuffer
	h := handler(touchFails{}, core.NewLocal(st), testLogger(&logs))
	base := startServer(t, h, nil)
	resp, body := send(t, client(nil), http.MethodPost, base+"/v1/agent/task_current", "",
		map[string]string{"X-Cairn-Project": "acme-api", "X-Cairn-Agent": "main",
			"Authorization": "Bearer cairn_" + strings.Repeat("A", 43)})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("call with a failed touch: status %d, want 200: %s", resp.StatusCode, body)
	}
	if text := logs.String(); !strings.Contains(text, "last_used_at was not written") || !strings.Contains(text, "touch broke") {
		t.Errorf("the failed touch was not logged: %q", text)
	}
}

// TestRedactRemovesKeysAndBearerCredentials checks the second guard on the logs: a cause
// that carries a key, or a bearer credential, is written without it.
func TestRedactRemovesKeysAndBearerCredentials(t *testing.T) {
	key, err := auth.NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	in := "auth: " + key + " and Bearer " + key + " end"
	out := redact(in)
	if strings.Contains(out, key) || strings.Contains(out, "Bearer "+key) {
		t.Errorf("redact left a key or a bearer credential: %q", out)
	}
	if !strings.Contains(out, "cairn_<redacted>") || !strings.Contains(out, "Bearer <redacted>") {
		t.Errorf("redact did not mark what it removed: %q", out)
	}
}

// selfSigned writes a self-signed certificate and its key to dir, as PEM, and returns the
// file names and the certificate PEM. Nothing is committed; the files are in the test's
// temporary directory.
func selfSigned(t *testing.T, dir string) (certFile, keyFile string, certPEM []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "cairn serve test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile, certPEM
}

// TestTLSServesWithACertificateAndRefusesTLS11 serves over TLS from certificate files, and
// checks two things: a client that trusts the certificate gets an answer, and a client that
// offers only TLS 1.1 gets no connection, since the server's minimum is 1.2 (D20).
func TestTLSServesWithACertificateAndRefusesTLS11(t *testing.T) {
	certFile, keyFile, certPEM := selfSigned(t, t.TempDir())
	cfg, err := LoadTLS(certFile, keyFile)
	if err != nil {
		t.Fatalf("LoadTLS: %v", err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x, want TLS 1.2", cfg.MinVersion)
	}
	st := openStore(t)
	base := startServer(t, Handler(st, testLogger(io.Discard)), cfg)

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("the certificate PEM did not load")
	}
	good := client(&http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}})
	resp, body := send(t, good, http.MethodGet, base+"/healthz", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("TLS 1.2 healthz: status %d: %s", resp.StatusCode, body)
	}
	if resp.TLS == nil || resp.TLS.Version < tls.VersionTLS12 {
		t.Errorf("the connection is not TLS 1.2 or later: %+v", resp.TLS)
	}

	old := client(&http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MaxVersion: tls.VersionTLS11}})
	if _, err := old.Get(base + "/healthz"); err == nil {
		t.Error("a TLS 1.1 client connected: the minimum version is not enforced")
	}
}

// TestLoadTLSRefusesAMissingFile checks that a missing certificate is an error that
// names the load, and no TLS settings come back.
func TestLoadTLSRefusesAMissingFile(t *testing.T) {
	cfg, err := LoadTLS(filepath.Join(t.TempDir(), "no-cert.pem"), filepath.Join(t.TempDir(), "no-key.pem"))
	if err == nil || cfg != nil {
		t.Fatalf("LoadTLS of missing files = %v, %v; want an error and no settings", cfg, err)
	}
}

// TestRunPrintsTheNoKeyLineOnLoopback starts a loopback server with no key. It prints one
// line that says every call is refused until a key exists, then stops at the deadline.
// It binds 127.0.0.1 only.
func TestRunPrintsTheNoKeyLineOnLoopback(t *testing.T) {
	var out syncBuffer
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err := Run(ctx, Options{Addr: "127.0.0.1:0", DB: filepath.Join(t.TempDir(), "cairn.db"), Log: &out})
	if err != nil {
		t.Fatalf("Run on loopback with no key: %v", err)
	}
	if !strings.Contains(out.String(), NoKeysLine) {
		t.Errorf("the no-key line is missing from the output: %q", out.String())
	}
}

// TestRunRefusesBeforeItListens checks the refusals that Run makes before it listens.
// None of these cases binds: each is refused, and the refusal is the test's assertion.
func TestRunRefusesBeforeItListens(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, _ := selfSigned(t, dir)
	db := filepath.Join(dir, "cairn.db")
	cases := []struct {
		what string
		o    Options
		want string
	}{
		{"no address", Options{DB: db}, "--addr is required"},
		{"no database", Options{Addr: "127.0.0.1:0"}, "--db is required"},
		{"a database path with ?", Options{Addr: "127.0.0.1:0", DB: db + "?mode=ro"}, "must not contain '?'"},
		{"a certificate without a key", Options{Addr: "127.0.0.1:0", DB: db, TLSCert: certFile}, "go together"},
		{"a key without a certificate", Options{Addr: "127.0.0.1:0", DB: db, TLSKey: keyFile}, "go together"},
		{"an address with no port", Options{Addr: "127.0.0.1", DB: db}, "--addr"},
		{"all interfaces, no TLS, no proxy", Options{Addr: "0.0.0.0:0", DB: db}, "needs TLS"},
		{"IPv6 all interfaces, no TLS", Options{Addr: "[::]:0", DB: db}, "needs TLS"},
		{"a LAN address, no TLS, behind a proxy but no key", Options{Addr: "192.168.1.5:0", DB: db, BehindProxy: true}, "at least one active API key"},
		{"all interfaces with TLS and no key", Options{Addr: "0.0.0.0:0", DB: db, TLSCert: certFile, TLSKey: keyFile}, "at least one active API key"},
	}
	for _, c := range cases {
		c.o.Log = io.Discard
		err := Run(context.Background(), c.o)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want a refusal naming %q", c.what, err, c.want)
		}
	}
}
