package main

import (
	"strings"
	"testing"
)

// TestSessionStartPrintsTheActiveTask checks the block a session start gets: the
// task, its next action, its current step, the directive count and the line that
// tells the agent to call task_current. It makes one call, a read.
func TestSessionStartPrintsTheActiveTask(t *testing.T) {
	var seen []string
	local, url := newServer(t, &seen)
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	id := seedTask(t, local, "acme-api")
	seen = nil

	code, stdout, stderr := runCmd(t, "hook", "session-start")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	for _, want := range []string{
		"Cairn task: \"Wire the export\" (" + id + ", active) in acme-api.\n",
		"Next action: Read the schema first\n",
		"Current step: 1 of 2, \"Read the schema\" (active).\n",
		"Directives that apply: 0.\n",
		"Call task_current for the brief, the steps and recent events.\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("session-start lacks %q:\n%s", want, stdout)
		}
	}
	if len(seen) != 1 || !strings.HasPrefix(seen[0], "/v1/agent/task_current") {
		t.Errorf("the hook made calls %q, want one task_current read", seen)
	}
}

// TestSessionStartWithNoActiveTaskIsOneLine checks the one line for a project with no
// active task.
func TestSessionStartWithNoActiveTaskIsOneLine(t *testing.T) {
	_, url := newServer(t, nil)
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")

	code, stdout, _ := runCmd(t, "hook", "session-start")
	if code != 0 || stdout != "Cairn: no task is active in acme-api.\n" {
		t.Errorf("exit %d, stdout %q: want the one line for no task", code, stdout)
	}
}

// TestSessionStartAddsNothingWhenUnconfigured checks that a session with no project,
// no server, or an unreachable server starts: exit 0, nothing on stdout, and the
// reason on stderr.
func TestSessionStartAddsNothingWhenUnconfigured(t *testing.T) {
	cases := map[string]func(t *testing.T){
		"no project and no server": func(t *testing.T) {
			t.Setenv("CAIRN_PROJECT", "")
			t.Setenv("CAIRN_SERVER", "")
		},
		"no server": func(t *testing.T) {
			t.Setenv("CAIRN_PROJECT", "acme-api")
			t.Setenv("CAIRN_SERVER", "")
		},
		"an unreachable server": func(t *testing.T) {
			t.Setenv("CAIRN_PROJECT", "acme-api")
			t.Setenv("CAIRN_SERVER", "http://127.0.0.1:1")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			useDir(t, t.TempDir())
			setup(t)
			code, stdout, stderr := runCmd(t, "hook", "session-start")
			if code != 0 {
				t.Errorf("exit %d, want 0 so the session starts (stderr %q)", code, stderr)
			}
			if stdout != "" {
				t.Errorf("stdout %q, want nothing: the context is not known", stdout)
			}
			if !strings.Contains(stderr, "no context added") {
				t.Errorf("stderr %q does not say no context was added", stderr)
			}
			assertNoDestructive(t, stderr)
		})
	}
}

// TestHookEventsAndArguments checks the events and the usage errors: session-start
// takes no arguments, pre-compact and stop are named as not built, and anything else
// is unknown. All exit 2.
func TestHookEventsAndArguments(t *testing.T) {
	cases := [][]string{
		{"hook"},
		{"hook", "pre-compact"},
		{"hook", "stop"},
		{"hook", "widget"},
		{"hook", "session-start", "extra"},
	}
	for _, args := range cases {
		code, stdout, stderr := runCmd(t, args...)
		if code != 2 || stdout != "" || stderr == "" {
			t.Errorf("%v: exit %d, stdout %q, stderr %q: want 2, no stdout and a message", args, code, stdout, stderr)
		}
	}
}
