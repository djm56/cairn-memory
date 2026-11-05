package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// agentProject makes a project with a project file, and runs the commands from it. useDir points HOME at a fresh
// directory, so the user's settings file is one the test writes beneath it. It returns the project root.
func agentProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, ".cairn.toml", "project = \"acme-api\"\n")
	useDir(t, dir)
	return dir
}

// sharedSettings is the project's settings.json, which Claude Code may commit, beside the local settings file.
func sharedSettings(root string) string {
	return filepath.Join(root, settingsDir, settingsFileName)
}

// userSettings is the user's settings file, under the home directory that useDir made.
func userSettings(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, ".claude", settingsFileName)
}

// checkLine is the line the --agent section prints for a check.
func checkLine(c check) string {
	return fmt.Sprintf("%-12s %-5s %s", c.name, c.status, c.detail)
}

// The entries a settings file holds for each cairn hook, one per event, with the matcher given.
const (
	sessionStartCompact = `{"hooks":{"SessionStart":[{"matcher":"startup|resume|clear|compact","hooks":[{"type":"command","command":"cairn hook session-start"}]}]}}`
	sessionStartOnly    = `{"hooks":{"SessionStart":[{"matcher":"startup","hooks":[{"type":"command","command":"cairn hook session-start"}]}]}}`
	preCompactEntry     = `{"hooks":{"PreCompact":[{"hooks":[{"type":"command","command":"cairn hook pre-compact"}]}]}}`
	stopEntry           = `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"cairn hook stop"}]}]}}`
)

// TestDoctorAgentFindsEachLocation checks that each cairn hook is reported under its event, with the file it is in and
// its matcher, when the three settings files hold one hook each.
func TestDoctorAgentFindsEachLocation(t *testing.T) {
	dir := agentProject(t)
	local := settingsIn(dir)
	shared := sharedSettings(dir)
	user := userSettings(t)
	writeSettings(t, local, sessionStartCompact)
	writeSettings(t, shared, preCompactEntry)
	writeSettings(t, user, stopEntry)

	want := []check{
		{name: "SessionStart", status: "ok", detail: "installed in " + local + ", matcher startup|resume|clear|compact"},
		{name: "PreCompact", status: "ok", detail: "installed in " + shared + ", no matcher; installed per D15, but Claude Code does not show its output, so this is an operator decision"},
		{name: "Stop", status: "ok", detail: "installed in " + user + ", no matcher"},
	}
	if got := claudeCodeChecks(dir); !reflect.DeepEqual(got, want) {
		t.Errorf("checks %v, want %v", got, want)
	}
	_, stdout, _ := runCmd(t, "doctor", "--agent", "claude-code")
	for _, c := range want {
		if !strings.Contains(stdout, checkLine(c)+"\n") {
			t.Errorf("output lacks %q:\n%s", checkLine(c), stdout)
		}
	}
}

// TestDoctorAgentNoneInstalled checks that a project with no cairn hooks reports each event as not installed, as a
// warning, and that no check fails.
func TestDoctorAgentNoneInstalled(t *testing.T) {
	dir := agentProject(t)

	checks := claudeCodeChecks(dir)
	for _, event := range []string{"SessionStart", "PreCompact", "Stop"} {
		var found bool
		for _, c := range checks {
			if c.name == event {
				found = true
				if c.status != "warn" || !strings.HasPrefix(c.detail, "not installed") {
					t.Errorf("%s is %s %q, want a warning that it is not installed", event, c.status, c.detail)
				}
			}
		}
		if !found {
			t.Errorf("no line for %s", event)
		}
	}
	if anyFailed(checks) {
		t.Errorf("a missing hook failed the check: %v", checks)
	}
}

// TestDoctorAgentMissingHookIsAWarning checks the exit code for a missing hook: a warning, so with a server that
// answers and the project found, the command exits 0 and prints the warning.
func TestDoctorAgentMissingHookIsAWarning(t *testing.T) {
	_, url := newServer(t, nil)
	dir := agentProject(t)
	writeFile(t, dir, ".cairn.toml", "project = \"acme-api\"\nserver = \""+url+"\"\n")

	code, stdout, stderr := runCmd(t, "doctor", "--agent", "claude-code")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q: want 0 with a missing hook, which is a warning", code, stderr)
	}
	if want := checkLine(check{name: "Stop", status: "warn", detail: "not installed in any settings file read: run cairn enable --agent claude-code"}); !strings.Contains(stdout, want+"\n") {
		t.Errorf("output lacks %q:\n%s", want, stdout)
	}
}

// TestDoctorAgentFlagsAMatcherThatDoesNotNameCompact checks that a SessionStart matcher which does not name compact is
// a warning that says why. It also checks which matchers cover compact.
func TestDoctorAgentFlagsAMatcherThatDoesNotNameCompact(t *testing.T) {
	cases := []struct {
		matcher string
		covers  bool
	}{
		{"", true},
		{"*", true},
		{"startup|resume|clear|compact", true},
		{"startup, compact", true},
		{"compact", true},
		{"startup", false},
		{"startup|resume|clear", false},
		{"compact.*", false},
		{"^compact$", false},
	}
	for _, tc := range cases {
		if got := coversCompact(tc.matcher); got != tc.covers {
			t.Errorf("coversCompact(%q) = %v, want %v", tc.matcher, got, tc.covers)
		}
	}

	dir := agentProject(t)
	local := settingsIn(dir)
	writeSettings(t, local, sessionStartOnly)
	var session check
	for _, c := range claudeCodeChecks(dir) {
		if c.name == "SessionStart" {
			session = c
		}
	}
	if session.status != "warn" || !strings.Contains(session.detail, "matcher startup does not name compact") {
		t.Errorf("SessionStart is %s %q, want a warning that the matcher does not name compact", session.status, session.detail)
	}
	if !strings.Contains(session.detail, local) {
		t.Errorf("the warning does not name the file: %q", session.detail)
	}
}

// TestDoctorAgentReportsAMalformedFile checks that a settings file enable would refuse is reported with its reason, as
// a warning. It is not fatal: the other files are still read, and the command does not fail on it.
func TestDoctorAgentReportsAMalformedFile(t *testing.T) {
	cases := []struct {
		name    string
		content string
		reason  string
	}{
		{"not valid JSON", `{"hooks": `, "is not valid JSON"},
		{"an empty file", ``, "is not valid JSON"},
		{"a top level that is an array", `[]`, "is not a JSON object"},
		{"hooks that is an array", `{"hooks": []}`, `has a "hooks" key that is not a JSON object`},
		{"an event that is an object", `{"hooks": {"SessionStart": {}}}`, `has a "hooks" entry for SessionStart that is not a JSON array`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := agentProject(t)
			local := settingsIn(dir)
			writeSettings(t, local, tc.content)
			writeSettings(t, sharedSettings(dir), preCompactEntry)
			writeSettings(t, userSettings(t), stopEntry)

			checks := claudeCodeChecks(dir)
			var malformed, pre, stop *check
			for i := range checks {
				c := &checks[i]
				switch {
				case c.name == "settings":
					malformed = c
				case c.name == "PreCompact":
					pre = c
				case c.name == "Stop":
					stop = c
				}
			}
			if malformed == nil || malformed.status != "warn" || !strings.Contains(malformed.detail, local+" "+tc.reason) {
				t.Errorf("malformed check is %+v, want a warning with %q", malformed, local+" "+tc.reason)
			}
			if pre == nil || pre.status != "ok" {
				t.Errorf("PreCompact in the other file is %+v, want it still read", pre)
			}
			if stop == nil || stop.status != "ok" {
				t.Errorf("Stop in the user's file is %+v, want it still read", stop)
			}
			if anyFailed(checks) {
				t.Errorf("a malformed file failed the check: %v", checks)
			}
		})
	}
}

// TestDoctorAgentWithNoProjectReadsTheUsersFile checks a directory with no project file: the project's settings are
// skipped with the reason, the user's file is still read, and nothing fails.
func TestDoctorAgentWithNoProjectReadsTheUsersFile(t *testing.T) {
	dir := t.TempDir()
	useDir(t, dir)
	writeSettings(t, userSettings(t), stopEntry)

	checks := claudeCodeChecks(dir)
	var skipped, stop bool
	for _, c := range checks {
		if c.name == "settings" && c.status == "skip" && strings.Contains(c.detail, "no project file") {
			skipped = true
		}
		if c.name == "Stop" && c.status == "ok" {
			stop = true
		}
	}
	if !skipped || !stop {
		t.Errorf("checks %v: want the project's settings skipped and the user's Stop read", checks)
	}
	if anyFailed(checks) {
		t.Errorf("a missing project failed the hook check: %v", checks)
	}
}

// TestDoctorAgentSectionFollowsTheExistingChecks checks that the section comes after every check doctor ran before, and
// that a failing existing check still sets the exit code.
func TestDoctorAgentSectionFollowsTheExistingChecks(t *testing.T) {
	agentProject(t)

	code, stdout, _ := runCmd(t, "doctor", "--agent", "claude-code")
	header := strings.Index(stdout, "hooks for claude-code\n")
	if header < 0 {
		t.Fatalf("no hooks section:\n%s", stdout)
	}
	if last := strings.LastIndex(stdout[:header], "reach"); last < 0 {
		t.Errorf("the section does not follow the reach check:\n%s", stdout)
	}
	if code != 1 {
		t.Errorf("exit %d, want 1 from the server check, which has no server here", code)
	}
}

// TestDoctorUsageErrors checks the usage errors. Each exits 2, and prints nothing on stdout, since a usage error is
// found before any check runs.
func TestDoctorUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"an agent that is not built", []string{"doctor", "--agent", "opencode"}, "not built yet"},
		{"pi is not built yet", []string{"doctor", "--agent", "pi"}, "not built yet"},
		{"an unknown agent", []string{"doctor", "--agent", "nope"}, "unknown agent"},
		{"an argument after the flags", []string{"doctor", "--agent", "claude-code", "extra"}, "takes flags only"},
		{"an unknown flag", []string{"doctor", "--nope"}, "run cairn doctor --help"},
		{"an agent with no value", []string{"doctor", "--agent"}, "run cairn doctor --help"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agentProject(t)

			code, stdout, stderr := runCmd(t, tc.args...)
			if code != 2 || stdout != "" || !strings.Contains(stderr, tc.want) {
				t.Errorf("exit %d, stdout %q, stderr %q: want 2, nothing on stdout and %q on stderr", code, stdout, stderr, tc.want)
			}
		})
	}
}

// serverProject is agentProject with a server: the project file names url, so the hooks and the reach check call it.
func serverProject(t *testing.T, url string) string {
	t.Helper()
	dir := agentProject(t)
	writeFile(t, dir, ".cairn.toml", "project = \"acme-api\"\nserver = \""+url+"\"\n")
	return dir
}

// TestDoctorDryRunWithAnActiveTask checks the dry run of the three hooks against a server with an active task. Each
// hook exits 0 and prints something; the session-start and pre-compact lines show the first line of the block, and the
// stop hook prints one JSON object with a systemMessage. Each hook makes one read of the active task, and the project's
// tree is left as it was.
func TestDoctorDryRunWithAnActiveTask(t *testing.T) {
	var seen []string
	local, url := newServer(t, &seen)
	dir := serverProject(t, url)
	id := seedTask(t, local, "acme-api")
	seen = nil

	checks := dryRunChecks()
	if len(checks) != 3 {
		t.Fatalf("%d dry runs, want one per hook: %v", len(checks), checks)
	}
	for i, prefix := range []string{"session-start: exit 0, ", "pre-compact: exit 0, ", "stop: exit 0, "} {
		if checks[i].status != "ok" || !strings.HasPrefix(checks[i].detail, prefix) {
			t.Errorf("dry run %d is %s %q, want ok and the prefix %q", i, checks[i].status, checks[i].detail, prefix)
		}
	}
	firstLine := "first line: Cairn task: \"Wire the export\" (" + id + ", active) in acme-api."
	for i := 0; i < 2; i++ {
		if !strings.Contains(checks[i].detail, firstLine) {
			t.Errorf("dry run %d lacks the first line of the block:\n%q", i, checks[i].detail)
		}
	}
	if !strings.Contains(checks[2].detail, "valid JSON with systemMessage") {
		t.Errorf("stop lacks the JSON check: %q", checks[2].detail)
	}
	if len(seen) != 3 {
		t.Errorf("the hooks made %d calls, want one read each: %q", len(seen), seen)
	}
	for _, p := range seen {
		if !strings.HasPrefix(p, "/v1/agent/task_current") {
			t.Errorf("a hook made a call other than a read: %q", p)
		}
	}
	if got := listDir(t, dir); !slices.Equal(got, []string{".cairn.toml"}) {
		t.Errorf("the project holds %v after the dry run, want the project file alone", got)
	}

	_, stdout, _ := runCmd(t, "doctor", "--agent", "claude-code")
	for _, c := range checks {
		if !strings.Contains(stdout, checkLine(c)+"\n") {
			t.Errorf("output lacks the dry run line %q:\n%s", checkLine(c), stdout)
		}
	}
}

// TestDoctorDryRunWithNoActiveTask checks the dry run against a server with no active task: session-start and
// pre-compact print the one line for that, and stop prints nothing, which is ok.
func TestDoctorDryRunWithNoActiveTask(t *testing.T) {
	_, url := newServer(t, nil)
	serverProject(t, url)

	checks := dryRunChecks()
	if len(checks) != 3 {
		t.Fatalf("%d dry runs, want one per hook: %v", len(checks), checks)
	}
	for i := 0; i < 2; i++ {
		if checks[i].status != "ok" || !strings.Contains(checks[i].detail, "first line: Cairn: no task is active in acme-api.") {
			t.Errorf("dry run %d is %s %q, want ok and the no-task line", i, checks[i].status, checks[i].detail)
		}
	}
	if checks[2].status != "ok" || !strings.Contains(checks[2].detail, "0 bytes, nothing printed") {
		t.Errorf("stop is %s %q, want ok and nothing printed", checks[2].status, checks[2].detail)
	}
}

// projectNames lists the names of the projects the server holds, as an operator reads the list. It is the server's own
// answer, so a test sees a row the hooks created.
func projectNames(t *testing.T, local *core.Local) []string {
	t.Helper()
	ctx := core.WithCaller(context.Background(), core.Caller{Role: core.RoleOperator})
	list, err := local.ProjectList(ctx, core.ProjectListRequest{})
	if err != nil {
		t.Fatalf("project list: %v", err)
	}
	names := []string{}
	for _, p := range list.Projects {
		names = append(names, p.Name)
	}
	return names
}

// TestDoctorDryRunCreatesTheProjectRow pins what the dry run does on the server. The server has never seen acme-api, so
// the first read of it creates the project's row, as the hooks do. The project list is read before and after.
func TestDoctorDryRunCreatesTheProjectRow(t *testing.T) {
	local, url := newServer(t, nil)
	serverProject(t, url)
	if before := projectNames(t, local); len(before) != 0 {
		t.Fatalf("the server lists %v before the dry run, want none", before)
	}

	dryRunChecks()

	if after := projectNames(t, local); !slices.Equal(after, []string{"acme-api"}) {
		t.Errorf("the server lists %v after the dry run, want the row the hooks create", after)
	}
}

// TestDoctorHelpSaysWhatTheDryRunWrites checks that the help says the dry run's first read can create the project's row,
// and that the command without --agent changes nothing.
func TestDoctorHelpSaysWhatTheDryRunWrites(t *testing.T) {
	_, stdout, _ := runCmd(t, "doctor", "--help")
	for _, want := range []string{"Without --agent it changes nothing.", "creates the project's row, as the hooks do."} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q:\n%s", want, stdout)
		}
	}
}

// TestDoctorDryRunWithNoServer checks the dry run with no server configured: each hook prints nothing and says why on
// stderr, so each is a warning, not a failure.
func TestDoctorDryRunWithNoServer(t *testing.T) {
	agentProject(t)

	checks := dryRunChecks()
	for i, c := range checks {
		if c.status != "warn" || !strings.Contains(c.detail, "nothing printed") || !strings.Contains(c.detail, "stderr: ") {
			t.Errorf("dry run %d is %s %q, want a warning that says why on stderr", i, c.status, c.detail)
		}
		if anyFailed([]check{c}) {
			t.Errorf("dry run %d failed with no server: %q", i, c.detail)
		}
	}
}

// TestStopOutputOK checks the JSON rule for the stop hook: one object with a systemMessage that is text. Anything else
// is refused, including JSON that is valid but has no such message.
func TestStopOutputOK(t *testing.T) {
	cases := []struct {
		out  string
		want bool
	}{
		{`{"systemMessage":"Cairn warning: a step has no summary."}`, true},
		{"{\"systemMessage\":\"x\"}\n", true},
		{`{"systemMessage":""}`, false},
		{`{"systemMessage":5}`, false},
		{`{}`, false},
		{`null`, false},
		{`[]`, false},
		{`"systemMessage"`, false},
		{`{"systemMessage":"x"} trailing`, false},
		{`not json`, false},
	}
	for _, tc := range cases {
		if got := stopOutputOK([]byte(tc.out)); got != tc.want {
			t.Errorf("stopOutputOK(%q) = %v, want %v", tc.out, got, tc.want)
		}
	}
}

// TestDoctorHelp checks that the help prints the usage and the flags on stdout, and exits 0.
func TestDoctorHelp(t *testing.T) {
	for _, flag := range []string{"--help", "-h"} {
		code, stdout, stderr := runCmd(t, "doctor", flag)
		if code != 0 || stderr != "" || !strings.Contains(stdout, "Usage: cairn doctor [--agent claude-code]") || !strings.Contains(stdout, "-agent string") {
			t.Errorf("doctor %s: exit %d, stdout %q, stderr %q", flag, code, stdout, stderr)
		}
	}
}
