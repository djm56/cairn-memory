// Package smokecheck tests scripts/smoke-check.sh. The script is run by bash against a stub
// cairn binary and a stub server on the loopback address, so the tests need no network and no
// install. The stub server records every call it gets, so a test can show the script calls only
// the read endpoints it names.
package smokecheck

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// scriptPath is the script under test, relative to this package.
const scriptPath = "../../scripts/smoke-check.sh"

// testKey is the key the stub server accepts. Its shape is a Cairn key's, so the script
// sends it. It is never expected in the script's output.
const testKey = "cairn_test_key_0123456789"

// stubBinary stands in for cairn. It prints a version, lists doctor in help, runs doctor with
// exit 0, and answers cairn mcp with the initialize and tools/list replies.
const stubBinary = `#!/usr/bin/env bash
case "$1" in
  version) echo "cairn test" ;;
  help) printf 'Commands:\n  doctor     Check the project file\n' ;;
  doctor) echo "binary   ok    cairn test"; exit 0 ;;
  mcp)
    cat > /dev/null
    echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"cairn","version":"test"}}}'
    echo '{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}'
    ;;
  *) echo "unexpected: $*" >&2; exit 2 ;;
esac
`

// readEndpoints are the only calls the script may make: the health probe and the one agent read.
var readEndpoints = map[string]bool{
	"GET /healthz":                true,
	"POST /v1/agent/task_current": true,
}

// stubServer is a Cairn server stand-in. It answers the health probe, refuses a call with no
// key, accepts testKey with the answer of a call that names no project, and refuses any other
// key. It records each call as "METHOD path".
type stubServer struct {
	mu    sync.Mutex
	calls []string
}

// ServeHTTP answers one call of the stub and records it.
func (s *stubServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.calls = append(s.calls, r.Method+" "+r.URL.Path)
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		fmt.Fprint(w, "{\"status\":\"ok\"}\n")
	case r.Method == http.MethodPost && r.URL.Path == "/v1/agent/task_current":
		auth := r.Header.Get("Authorization")
		switch auth {
		case "":
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":{"kind":"unauthenticated","message":"the request needs a valid API key"}}`)
		case "Bearer " + testKey:
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"kind":"invalid","message":"no project"}}`)
		default:
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":{"kind":"unauthenticated","message":"the request needs a valid API key"}}`)
		}
	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"kind":"no_route","message":"no such call"}}`)
	}
}

// recorded returns the calls the stub has received, in order.
func (s *stubServer) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// setup writes the stub binary into a fresh directory, starts a stub server on the loopback
// address, and returns both, with bash and curl checked. A test that needs curl or bash is skipped
// where either is missing.
func setup(t *testing.T) (binPath string, stub *stubServer, srv *httptest.Server) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not on PATH, so the smoke check cannot run")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not on PATH, so the server checks cannot run")
	}
	binPath = filepath.Join(t.TempDir(), "cairn")
	if err := os.WriteFile(binPath, []byte(stubBinary), 0o700); err != nil {
		t.Fatal(err)
	}
	stub = &stubServer{}
	srv = httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	return binPath, stub, srv
}

// runScript runs the script with args and env, and returns its output and exit code. env holds
// KEY=value pairs; PATH and HOME are set for the run, and no other variable from the test process
// reaches the script, so CAIRN_API_KEY and CAIRN_SERVER are only what a test sets.
func runScript(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, append([]string{scriptPath}, args...)...)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}, env...)
	out, err := cmd.CombinedOutput()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run the script: %v", err)
	}
	return string(out), code
}

// hasStatus reports whether out has a line that starts with status and then the check name.
func hasStatus(out, status, check string) bool {
	re := regexp.MustCompile(`(?m)^` + status + `\s+` + check + `\b`)
	return re.MatchString(out)
}

// TestSmokeCheckPassesAgainstAGoodInstall is the pass case: every check passes, the script exits 0,
// the key is never printed, and the server saw only the two read endpoints.
func TestSmokeCheckPassesAgainstAGoodInstall(t *testing.T) {
	bin, stub, srv := setup(t)
	out, code := runScript(t, []string{"CAIRN_API_KEY=" + testKey}, "--bin", bin, "--server", srv.URL)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	for _, check := range []string{"binary", "version", "doctor", "healthz", "refused", "key", "mcp"} {
		if !hasStatus(out, "PASS", check) {
			t.Errorf("no PASS line for %s\n%s", check, out)
		}
	}
	if strings.Contains(out, "FAIL") {
		t.Errorf("a FAIL line in a good run\n%s", out)
	}
	if !strings.Contains(out, "smoke-check: 7 passed, 0 failed, 0 skipped") {
		t.Errorf("summary line not as expected\n%s", out)
	}
	if strings.Contains(out, testKey) {
		t.Errorf("the output prints the API key\n%s", out)
	}
	calls := stub.recorded()
	if len(calls) != 3 {
		t.Errorf("the server got %d calls, want 3 (healthz, refused, key): %v", len(calls), calls)
	}
	for _, c := range calls {
		if !readEndpoints[c] {
			t.Errorf("the script called %q, which is not one of the read endpoints", c)
		}
	}
}

// TestSmokeCheckFailsWhenTheServerIsDown is the server-down case: the server is started and closed,
// so its address refuses connections. healthz and the other server checks fail, and the script
// exits 1.
func TestSmokeCheckFailsWhenTheServerIsDown(t *testing.T) {
	bin, _, srv := setup(t)
	url := srv.URL
	srv.Close()
	out, code := runScript(t, []string{"CAIRN_API_KEY=" + testKey}, "--bin", bin, "--server", url)
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	if !hasStatus(out, "FAIL", "healthz") {
		t.Errorf("no FAIL line for healthz\n%s", out)
	}
	if !hasStatus(out, "FAIL", "refused") {
		t.Errorf("no FAIL line for refused\n%s", out)
	}
	if !strings.Contains(out, "smoke-check:") {
		t.Errorf("no summary line\n%s", out)
	}
}

// TestSmokeCheckSkipsTheKeyChecksWithoutAKey is the missing-key case: the server checks that need
// a key are SKIP, the unauthenticated refusal still runs, and the run is not a failure.
func TestSmokeCheckSkipsTheKeyChecksWithoutAKey(t *testing.T) {
	bin, stub, srv := setup(t)
	out, code := runScript(t, nil, "--bin", bin, "--server", srv.URL)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (a skip is not a failure)\n%s", code, out)
	}
	if !hasStatus(out, "SKIP", "key") {
		t.Errorf("no SKIP line for key\n%s", out)
	}
	if !hasStatus(out, "SKIP", "mcp") {
		t.Errorf("no SKIP line for mcp\n%s", out)
	}
	if !hasStatus(out, "PASS", "refused") {
		t.Errorf("the refusal check did not pass without a key\n%s", out)
	}
	if got := len(stub.recorded()); got != 2 {
		t.Errorf("the server got %d calls without a key, want 2 (healthz, refused): %v", got, stub.recorded())
	}
}

// TestSmokeCheckFailsAKeyTheServerRefuses checks that a key the server does not accept fails the key
// check and names no key in the output.
func TestSmokeCheckFailsAKeyTheServerRefuses(t *testing.T) {
	bin, _, srv := setup(t)
	const wrong = "cairn_wrong_key_0123456789"
	out, code := runScript(t, []string{"CAIRN_API_KEY=" + wrong}, "--bin", bin, "--server", srv.URL)
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	if !hasStatus(out, "FAIL", "key") {
		t.Errorf("no FAIL line for key\n%s", out)
	}
	if strings.Contains(out, wrong) {
		t.Errorf("the output prints the key\n%s", out)
	}
}

// TestSmokeCheckFailsWhenTheBinaryIsMissing checks that a binary that is not there fails its check,
// and that the server checks still run.
func TestSmokeCheckFailsWhenTheBinaryIsMissing(t *testing.T) {
	_, _, srv := setup(t)
	out, code := runScript(t, []string{"CAIRN_API_KEY=" + testKey}, "--bin", filepath.Join(t.TempDir(), "no-such-cairn"), "--server", srv.URL)
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, out)
	}
	if !hasStatus(out, "FAIL", "binary") {
		t.Errorf("no FAIL line for binary\n%s", out)
	}
	if !hasStatus(out, "PASS", "healthz") {
		t.Errorf("the server checks did not run without a binary\n%s", out)
	}
}

// TestSmokeCheckUsageErrorsExitTwo checks that each misuse exits 2, with a message on stderr.
func TestSmokeCheckUsageErrorsExitTwo(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"an unknown flag", []string{"--bogus"}},
		{"a flag with no value", []string{"--server"}},
		{"a server that is not http", []string{"--server", "ftp://example.test"}},
		{"a key given as a flag", []string{"--key", testKey}},
		{"a positional argument", []string{"extra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, code := runScript(t, nil, tc.args...)
			if code != 2 {
				t.Fatalf("exit = %d, want 2\n%s", code, out)
			}
			if strings.Contains(out, testKey) {
				t.Errorf("the usage error prints the key\n%s", out)
			}
		})
	}
}

// TestSmokeCheckHelpNamesTheReadsAndTheKey checks that --help exits 0 and says where the key comes
// from and which endpoints the script reads.
func TestSmokeCheckHelpNamesTheReadsAndTheKey(t *testing.T) {
	out, code := runScript(t, nil, "--help")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	for _, want := range []string{"CAIRN_API_KEY", "/healthz", "/v1/agent/task_current", "--server URL"} {
		if !strings.Contains(out, want) {
			t.Errorf("--help does not mention %q\n%s", want, out)
		}
	}
}
