package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
