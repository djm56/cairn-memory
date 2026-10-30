package main

import (
	"strings"
	"testing"
)

// TestStatusShowsTheActiveTask checks the status block for a project with an active
// task: the project and server with their sources, the task, its next action and its
// current step.
func TestStatusShowsTheActiveTask(t *testing.T) {
	local, url := newServer(t, nil)
	dir := t.TempDir()
	useDir(t, dir)
	writeFile(t, dir, ".cairn.toml", "project = \"acme-api\"\nserver = \""+url+"\"\n")
	id := seedTask(t, local, "acme-api")

	code, stdout, stderr := runCmd(t, "status")
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stderr %q)", code, stderr)
	}
	for _, want := range []string{
		"project     acme-api (from " + dir + "/.cairn.toml)",
		"server      " + url + " (from .cairn.toml)",
		"task        " + id + "  active  Wire the export",
		"next        Read the schema first",
		"step        1 of 2: Read the schema (active)",
		"directives  0 apply to this project",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("status output lacks %q:\n%s", want, stdout)
		}
	}
}

// TestStatusWithNoActiveTask checks the one line for a project with no task.
func TestStatusWithNoActiveTask(t *testing.T) {
	_, url := newServer(t, nil)
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")

	code, stdout, stderr := runCmd(t, "status")
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stdout, "task        none active") || !strings.Contains(stdout, "(from CAIRN_PROJECT)") || !strings.Contains(stdout, "(from CAIRN_SERVER)") {
		t.Errorf("status output %q: want no task, and both sources named from the environment", stdout)
	}
}

// TestStatusNamesTheFixWhenUnconfigured checks that a missing project and a missing
// server each exit 1 and name the fix, and write nothing to stdout.
func TestStatusNamesTheFixWhenUnconfigured(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)

	code, stdout, stderr := runCmd(t, "status")
	if code != 1 || !strings.Contains(stderr, "cairn init") || stdout != "" {
		t.Errorf("no project: exit %d, stdout %q, stderr %q: want 1, a named fix and no stdout", code, stdout, stderr)
	}
	t.Setenv("CAIRN_PROJECT", "acme-api")
	code, stdout, stderr = runCmd(t, "status")
	if code != 1 || !strings.Contains(stderr, "CAIRN_SERVER") || stdout != "" {
		t.Errorf("no server: exit %d, stdout %q, stderr %q: want 1 and the CAIRN_SERVER fix", code, stdout, stderr)
	}
	assertNoDestructive(t, stderr)
}

// TestStatusReportsAnUnreachableServer checks that a server that does not answer is
// a failure of the command, with the cause, and not a hang.
func TestStatusReportsAnUnreachableServer(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	// Port 1 on loopback has no listener, so the connection is refused at once.
	t.Setenv("CAIRN_SERVER", "http://127.0.0.1:1")

	code, stdout, stderr := runCmd(t, "status")
	if code != 1 || !strings.Contains(stderr, "client:") || stdout != "" {
		t.Errorf("exit %d, stdout %q, stderr %q: want 1, the client's error and no stdout", code, stdout, stderr)
	}
}

// TestStatusTakesNoArguments checks the usage error.
func TestStatusTakesNoArguments(t *testing.T) {
	code, _, stderr := runCmd(t, "status", "--json")
	if code != 2 || !strings.Contains(stderr, "takes no arguments") {
		t.Errorf("exit %d, stderr %q: want 2 and the usage error", code, stderr)
	}
}
