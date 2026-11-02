package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestDoctorWithNothingConfigured checks that every check runs and reports, and the
// command exits 1 with the fixes named.
func TestDoctorWithNothingConfigured(t *testing.T) {
	useDir(t, t.TempDir())

	code, stdout, stderr := runCmd(t, "doctor")
	if code != 1 || stderr != "" {
		t.Errorf("exit %d, stderr %q: want 1 and nothing on stderr", code, stderr)
	}
	for _, want := range []string{"binary", "project  fail", "server   fail", "reach    skip"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("doctor output lacks %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stdout, "cairn init") {
		t.Errorf("doctor output does not name cairn init:\n%s", stdout)
	}
	assertNoDestructive(t, stdout)
}

// TestDoctorPassesAgainstAServerAndChangesNothing checks that doctor passes against
// a real server, and that the reachability call carries no project. So the server
// saw no call that could create or read a project row, and the file is untouched.
func TestDoctorPassesAgainstAServerAndChangesNothing(t *testing.T) {
	var seen []string
	_, url := newServer(t, &seen)
	dir := t.TempDir()
	useDir(t, dir)
	path := writeFile(t, dir, ".cairn.toml", "project = \"acme-api\"\nserver = \""+url+"\"\n")
	before := readBytes(t, path)

	code, stdout, stderr := runCmd(t, "doctor")
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stdout %q, stderr %q)", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "reach    ok") {
		t.Errorf("doctor output lacks a passing reach:\n%s", stdout)
	}
	if len(seen) != 1 || !strings.HasSuffix(seen[0], "project=") {
		t.Errorf("requests seen by the server: %q, want one call with no project", seen)
	}
	if string(readBytes(t, path)) != string(before) {
		t.Errorf("doctor changed the project file")
	}
}

// TestDoctorFailsOnAServerThatIsNotCairn checks that a server answering 404 to
// everything fails the reach check, and the command exits 1.
func TestDoctorFailsOnAServerThatIsNotCairn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CAIRN_SERVER", srv.URL)

	code, stdout, _ := runCmd(t, "doctor")
	if code != 1 || !strings.Contains(stdout, "reach    fail") || !strings.Contains(stdout, "HTTP 404") {
		t.Errorf("exit %d, output %q: want 1 and a failing reach with the status", code, stdout)
	}
}

// TestDoctorFailsOnAProjectFileThatIsBroken checks that a broken project file is a
// failing check with the file named, and that doctor still reports the server.
func TestDoctorFailsOnAProjectFileThatIsBroken(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	writeFile(t, dir, ".cairn.toml", "project = [\n")
	code, stdout, _ := runCmd(t, "doctor")
	if code != 1 || !strings.Contains(stdout, "project  fail") || !strings.Contains(stdout, ".cairn.toml is not valid TOML") {
		t.Errorf("exit %d, output %q: want 1 and the broken file named", code, stdout)
	}
	assertNoDestructive(t, stdout)
}

// TestDoctorTakesNoArguments checks the usage error, since --agent is not built yet.
func TestDoctorTakesNoArguments(t *testing.T) {
	code, _, stderr := runCmd(t, "doctor", "--agent", "claude-code")
	if code != 2 || !strings.Contains(stderr, "takes no arguments yet") {
		t.Errorf("exit %d, stderr %q: want 2 and the usage error", code, stderr)
	}
}

// TestDoctorDoesNotFollowARedirect checks the redirect rule (step 8 review W3) at the command: a
// server that redirects the reach call to another server makes reach fail, and the other server
// receives no request, so the key is not sent there. The output holds no key.
func TestDoctorDoesNotFollowARedirect(t *testing.T) {
	var mu sync.Mutex
	var reached []string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reached = append(reached, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL+r.URL.Path)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)
	dir := t.TempDir()
	useDir(t, dir)
	writeFile(t, dir, ".cairn.toml", "project = \"acme-api\"\nserver = \""+origin.URL+"\"\n")

	code, out, _ := runCmd(t, "doctor")
	if code != 1 || !strings.Contains(out, "reach    fail") || !strings.Contains(out, "redirect") {
		t.Errorf("a redirecting server: exit %d, output:\n%s; want 1, a failing reach that names the redirect", code, out)
	}
	mu.Lock()
	n := len(reached)
	mu.Unlock()
	if n != 0 {
		t.Errorf("the redirect target received %d requests; want none", n)
	}
	if key := os.Getenv("CAIRN_API_KEY"); strings.Contains(out, key) {
		t.Errorf("doctor printed the key")
	}
}

// TestDoctorWarnsAboutAKeyInClearText checks 8-S9 at the command: a key found for an http server
// on a host that is not loopback is warned about, on a transport line that does not fail the
// check; a loopback server gets no such line. The host here does not resolve, so reach fails on
// its own and the warning is read from the output beside it. The output holds no key.
func TestDoctorWarnsAboutAKeyInClearText(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	// 192.0.2.1 is reserved for documentation (RFC 5737), so no real host answers it. It is not a
	// name, so no resolver can send the test key to a host of its own choosing (10-S7). Doctor's
	// reach call gives up at its timeout, which this test shortens (12-S3): the warning is read
	// from the output beside it, and the run does not wait out the real five seconds.
	prevTimeout := doctorTimeout
	doctorTimeout = 200 * time.Millisecond
	t.Cleanup(func() { doctorTimeout = prevTimeout })
	writeFile(t, dir, ".cairn.toml", "project = \"acme-api\"\nserver = \"http://192.0.2.1:8788\"\n")
	code, out, _ := runCmd(t, "doctor")
	if code != 1 || !strings.Contains(out, "transport warn") || !strings.Contains(out, "in clear text") {
		t.Errorf("a LAN-style http server: exit %d, output:\n%s; want the transport warning", code, out)
	}
	if key := os.Getenv("CAIRN_API_KEY"); strings.Contains(out, key) {
		t.Errorf("doctor printed the key")
	}

	writeFile(t, dir, ".cairn.toml", "project = \"acme-api\"\nserver = \"http://127.0.0.1:1\"\n")
	_, out, _ = runCmd(t, "doctor")
	if strings.Contains(out, "transport") {
		t.Errorf("a loopback server got a transport line:\n%s", out)
	}
}

// TestAWarningDoesNotFailTheCheck checks the exit rule of cairn doctor: a warn line is printed
// and leaves the exit code at 0, and only a fail line makes it 1 (10-S6).
func TestAWarningDoesNotFailTheCheck(t *testing.T) {
	cases := []struct {
		name   string
		checks []check
		failed bool
	}{
		{"all ok", []check{{name: "a", status: "ok"}, {name: "b", status: "ok"}}, false},
		{"a warning only", []check{{name: "a", status: "ok"}, {name: "transport", status: "warn"}}, false},
		{"a skip only", []check{{name: "reach", status: "skip"}}, false},
		{"a fail", []check{{name: "a", status: "ok"}, {name: "key", status: "fail"}}, true},
		{"a warning and a fail", []check{{name: "transport", status: "warn"}, {name: "key", status: "fail"}}, true},
	}
	for _, c := range cases {
		if got := anyFailed(c.checks); got != c.failed {
			t.Errorf("%s: anyFailed = %v, want %v", c.name, got, c.failed)
		}
	}
}
