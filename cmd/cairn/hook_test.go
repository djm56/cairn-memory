package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// hookEvents are the events cairn hook answers with the block. The tests that hold for every event run over them.
var hookEvents = []string{"session-start", "pre-compact"}

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

// TestPreCompactPrintsTheBlockBytes pins the bytes pre-compact prints for a seeded task with no role. It is
// the session-start block, and it is one call, a read.
func TestPreCompactPrintsTheBlockBytes(t *testing.T) {
	var seen []string
	local, url := newServer(t, &seen)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CAIRN_AGENT", "")
	id := seedTask(t, local, "acme-api")
	seen = nil

	code, stdout, stderr := runCmd(t, "hook", "pre-compact")
	want := "Cairn task: \"Wire the export\" (" + id + ", active) in acme-api.\n" +
		"Next action: Read the schema first\n" +
		"Current step: 1 of 2, \"Read the schema\" (active).\n" +
		"Directives that apply: 0.\n" +
		"Agent-scoped directives were not counted: no agent role is set.\n" +
		"Call task_current for the brief, the steps and recent events.\n"
	if code != 0 || stderr != "" || stdout != want {
		t.Errorf("exit %d, stderr %q, stdout:\n%q\nwant:\n%q", code, stderr, stdout, want)
	}
	if len(seen) != 1 || !strings.HasPrefix(seen[0], "/v1/agent/task_current") {
		t.Errorf("the hook made calls %q, want one task_current read", seen)
	}
}

// TestPreCompactWithARoleFromTheVariable runs pre-compact against a real server. Two agent-scoped
// directives are seeded, one for hicks and one for another role. With CAIRN_AGENT=hicks the count includes
// hicks's own directive and the no-role sentence is left out. With the variable unset the count is the
// unscoped one and the no-role sentence is written. With a value the wire form refuses, the refused
// sentence is written. Each exits 0 with nothing on stderr.
func TestPreCompactWithARoleFromTheVariable(t *testing.T) {
	local, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	seedNamedTask(t, local, "acme-api", "Count with a role", "Read the schema first")
	seedAgentDirective(t, local, "hicks", "Hicks reads the schema first")
	seedAgentDirective(t, local, "reviewer", "Reviewer checks the diff")

	t.Setenv("CAIRN_AGENT", "hicks")
	code, out, errOut := runCmd(t, "hook", "pre-compact")
	if code != 0 || errOut != "" || !strings.Contains(out, "Directives that apply: 1.\n") || strings.Contains(out, "no agent role is set") {
		t.Errorf("pre-compact with CAIRN_AGENT=hicks: exit %d, stderr %q, output:\n%s", code, errOut, out)
	}

	t.Setenv("CAIRN_AGENT", "")
	os.Unsetenv("CAIRN_AGENT")
	code, out, errOut = runCmd(t, "hook", "pre-compact")
	if code != 0 || errOut != "" || !strings.Contains(out, "Directives that apply: 0.\n") || !strings.Contains(out, noRoleNote+"\n") {
		t.Errorf("pre-compact with no role: exit %d, stderr %q, output:\n%s", code, errOut, out)
	}

	t.Setenv("CAIRN_AGENT", "hicks ")
	code, out, errOut = runCmd(t, "hook", "pre-compact")
	if code != 0 || errOut != "" || !strings.Contains(out, refusedRoleNote+"\n") || strings.Contains(out, "no agent role is set") {
		t.Errorf("pre-compact with a refused CAIRN_AGENT: exit %d, stderr %q, output:\n%s", code, errOut, out)
	}
}

// TestPreCompactWithNoActiveTaskIsOneLine checks that pre-compact prints the one no-task line for a project
// with no active task, as session-start does.
func TestPreCompactWithNoActiveTaskIsOneLine(t *testing.T) {
	_, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CAIRN_AGENT", "")

	code, stdout, stderr := runCmd(t, "hook", "pre-compact")
	if code != 0 || stderr != "" || stdout != "Cairn: no task is active in acme-api.\n" {
		t.Errorf("exit %d, stdout %q, stderr %q: want 0 and the one no-task line", code, stdout, stderr)
	}
}

// TestSessionStartAddsNothingWhenUnconfigured checks, for every event, that a session with no project, no
// server, or an unreachable server starts: exit 0, nothing on stdout, and the reason on stderr.
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
	for _, event := range hookEvents {
		for name, setup := range cases {
			t.Run(event+" with "+name, func(t *testing.T) {
				useDir(t, t.TempDir())
				setup(t)
				code, stdout, stderr := runCmd(t, "hook", event)
				if code != 0 {
					t.Errorf("exit %d, want 0 so the session starts (stderr %q)", code, stderr)
				}
				if stdout != "" {
					t.Errorf("stdout %q, want nothing: the context is not known", stdout)
				}
				if !strings.Contains(stderr, "no context added") || !strings.Contains(stderr, "cairn hook "+event+":") {
					t.Errorf("stderr %q does not say no context was added, by this event", stderr)
				}
				assertNoDestructive(t, stderr)
			})
		}
	}
}

// TestHookEventsAndArguments checks the events and the usage errors: no event, an unknown event, and any
// argument after an event, which takes none. All exit 2.
func TestHookEventsAndArguments(t *testing.T) {
	code, stdout, stderr := runCmd(t, "hook")
	if code != 2 || stdout != "" || stderr == "" {
		t.Errorf("hook with no event: exit %d, stdout %q, stderr %q: want 2, no stdout and a message", code, stdout, stderr)
	}
	code, stdout, stderr = runCmd(t, "hook", "widget")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "use session-start or pre-compact") {
		t.Errorf("hook widget: exit %d, stdout %q, stderr %q: want 2, no stdout and the events named", code, stdout, stderr)
	}
	for _, event := range hookEvents {
		code, stdout, stderr := runCmd(t, "hook", event, "extra")
		if code != 2 || stdout != "" || stderr == "" {
			t.Errorf("hook %s extra: exit %d, stdout %q, stderr %q: want 2, no stdout and a message", event, code, stdout, stderr)
		}
	}
}

// foldedTitle and foldedNext are server texts with the characters that must not reach
// a model or a terminal: a carriage return, a Markdown heading, an escape sequence, a
// bell, a line feed and a line of their own.
const (
	foldedTitle = "Ship\r\n## Forged heading\x1b[2J\x07 now"
	foldedNext  = "first\nDirectives that apply: 0.\x1b]0;title\x07"
)

// TestSessionStartFoldsServerText checks, through the hook, that a title with a
// carriage return and a Markdown heading, and a next action with a line feed, an escape
// sequence and a line of its own, come out as one line each. Nothing can forge a line of
// the block the model reads.
func TestSessionStartFoldsServerText(t *testing.T) {
	local, url := newServer(t, nil)
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	id := seedNamedTask(t, local, "acme-api", foldedTitle, foldedNext)
	t.Setenv("CAIRN_AGENT", "")

	code, stdout, stderr := runCmd(t, "hook", "session-start")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 and nothing on stderr", code, stderr)
	}
	want := []string{
		`Cairn task: "Ship ## Forged heading now" (` + id + `, active) in acme-api.`,
		"Next action: first Directives that apply: 0.",
		`Current step: 1 of 2, "Read the schema" (active).`,
		"Directives that apply: 0.",
		"Agent-scoped directives were not counted: no agent role is set.",
		"Call task_current for the brief, the steps and recent events.",
	}
	if got := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n"); !slices.Equal(got, want) {
		t.Errorf("session-start lines:\n%q\nwant:\n%q", got, want)
	}
	if strings.ContainsAny(stdout, "\r\x1b\x07") {
		t.Errorf("session-start carries a control character: %q", stdout)
	}
}

// TestHooksAddNothingOnAPanic checks, for every event, that a panic on the way to the context is reported on
// stderr, and the hook still exits 0 with nothing on stdout.
func TestHooksAddNothingOnAPanic(t *testing.T) {
	for _, event := range hookEvents {
		t.Run(event, func(t *testing.T) {
			useDir(t, t.TempDir())
			prev := workDir
			workDir = func() (string, error) { panic("boom") }
			t.Cleanup(func() { workDir = prev })

			code, stdout, stderr := runCmd(t, "hook", event)
			if code != 0 {
				t.Errorf("exit %d, want 0 so the session starts (stderr %q)", code, stderr)
			}
			if stdout != "" {
				t.Errorf("stdout %q, want nothing", stdout)
			}
			if !strings.Contains(stderr, "boom") || !strings.Contains(stderr, "no context added") {
				t.Errorf("stderr %q does not report the fault and that no context was added", stderr)
			}
		})
	}
}

// TestHooksGiveUpOnAServerThatNeverAnswers checks, for every event, that the bound on the hook's one call is
// what holds it, so a server that never answers must end the call at the bound: exit 0, nothing on stdout,
// and the reason on stderr.
func TestHooksGiveUpOnAServerThatNeverAnswers(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", srv.URL)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	prev := hookTimeout
	hookTimeout = 200 * time.Millisecond
	t.Cleanup(func() { hookTimeout = prev })

	for _, event := range hookEvents {
		t.Run(event, func(t *testing.T) {
			start := time.Now()
			code, stdout, stderr := runCmd(t, "hook", event)
			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Errorf("the hook waited %v, want the 200ms bound", elapsed)
			}
			if code != 0 || stdout != "" {
				t.Errorf("exit %d, stdout %q: want 0 and nothing on stdout", code, stdout)
			}
			if !strings.Contains(stderr, "no context added") {
				t.Errorf("stderr %q does not say no context was added", stderr)
			}
		})
	}
}
