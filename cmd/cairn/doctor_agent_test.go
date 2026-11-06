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

// TestDoctorAgentNoneInstalled checks that a project with no cairn hooks reports SessionStart and Stop as not installed,
// as a warning. PreCompact is an operator decision, so its absence is ok, with the note. No check fails.
func TestDoctorAgentNoneInstalled(t *testing.T) {
	dir := agentProject(t)

	checks := claudeCodeChecks(dir)
	for _, event := range []string{"SessionStart", "PreCompact", "Stop"} {
		var found bool
		for _, c := range checks {
			if c.name == event {
				found = true
				want := "warn"
				if event == "PreCompact" {
					want = "ok"
				}
				if c.status != want || !strings.HasPrefix(c.detail, "not installed") {
					t.Errorf("%s is %s %q, want %s, saying it is not installed", event, c.status, c.detail, want)
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

// TestDoctorAgentWarnsOnANonStringMatcher checks a matcher that is there but is not a string: doctor cannot read it, so
// the line is a warning that says why. A matcher that is null is no matcher, as the settings format reads it.
func TestDoctorAgentWarnsOnANonStringMatcher(t *testing.T) {
	cases := []struct {
		name   string
		entry  string
		status string
	}{
		{"a number", `"matcher":5,`, "warn"},
		{"an object", `"matcher":{"x":1},`, "warn"},
		{"null", `"matcher":null,`, "ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := agentProject(t)
			writeSettings(t, settingsIn(dir), `{"hooks":{"SessionStart":[{`+tc.entry+`"hooks":[{"type":"command","command":"cairn hook session-start"}]}]}}`)

			session := checkNamed(claudeCodeChecks(dir), "SessionStart")
			if session == nil || session.status != tc.status {
				t.Fatalf("SessionStart is %+v, want %s", session, tc.status)
			}
			if tc.status == "warn" && !strings.Contains(session.detail, "its matcher is not a string") {
				t.Errorf("the warning lacks the reason: %q", session.detail)
			}
		})
	}
}

// settingsSkip returns the settings check that skips, or nil when there is none.
func settingsSkip(checks []check) *check {
	for i := range checks {
		if checks[i].name == "settings" && checks[i].status == "skip" {
			return &checks[i]
		}
	}
	return nil
}

// TestDoctorAgentNamesWhyThereAreNoProjectSettings checks the skip line for the project's settings. A directory with no
// project file says so. CAIRN_PROJECT with no file says that. A project file that cannot be read gives its reason, and
// does not say there is no project file.
func TestDoctorAgentNamesWhyThereAreNoProjectSettings(t *testing.T) {
	t.Run("no project file", func(t *testing.T) {
		dir := t.TempDir()
		useDir(t, dir)
		want := "no project file, so the project's settings are not read"
		if c := settingsSkip(claudeCodeChecks(dir)); c == nil || c.detail != want {
			t.Errorf("skip is %+v, want %q", c, want)
		}
	})
	t.Run("CAIRN_PROJECT with no file", func(t *testing.T) {
		dir := t.TempDir()
		useDir(t, dir)
		t.Setenv("CAIRN_PROJECT", "acme-api")
		want := "CAIRN_PROJECT names the project, but there is no project file to find its settings from, so the project's settings are not read"
		if c := settingsSkip(claudeCodeChecks(dir)); c == nil || c.detail != want {
			t.Errorf("skip is %+v, want %q", c, want)
		}
	})
	t.Run("a project file that is broken", func(t *testing.T) {
		dir := agentProject(t)
		writeFile(t, dir, ".cairn.toml", "project = 5\n")
		c := settingsSkip(claudeCodeChecks(dir))
		if c == nil || strings.Contains(c.detail, "no project file") || !strings.Contains(c.detail, "could not be read") {
			t.Errorf("skip is %+v, want the reason the file could not be read, not no project file", c)
		}
	})
}

// TestJudgeDryRunWarnsOnSilence checks the rule for a dry run that prints nothing. session-start and pre-compact always
// print their block when they work, so silence is a warning, with stderr or without it. stop may print nothing and be
// ok, unless stderr says why.
func TestJudgeDryRunWarnsOnSilence(t *testing.T) {
	cases := []struct {
		arg    string
		errOut []byte
		status string
	}{
		{"session-start", nil, "warn"},
		{"pre-compact", nil, "warn"},
		{"session-start", []byte("cairn: no server\n"), "warn"},
		{"stop", nil, "ok"},
		{"stop", []byte("cairn: no server\n"), "warn"},
	}
	for _, tc := range cases {
		if c := judgeDryRun(tc.arg, 0, nil, tc.errOut); c.status != tc.status {
			t.Errorf("%s with stderr %q is %s %q, want %s", tc.arg, tc.errOut, c.status, c.detail, tc.status)
		}
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
		{"not plain JSON", `{"hooks": `, "is not plain JSON (comments or a syntax error)"},
		{"an empty file", ``, "is not plain JSON (comments or a syntax error)"},
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

// assertSettingsWarning fails the test unless checks holds a settings warning whose detail is exactly want.
func assertSettingsWarning(t *testing.T, checks []check, want string) {
	t.Helper()
	for _, c := range checks {
		if c.name == "settings" && c.status == "warn" && c.detail == want {
			return
		}
	}
	t.Errorf("no settings warning %q in %v", want, checks)
}

// checkNamed returns the check with the name, or nil when there is none.
func checkNamed(checks []check, name string) *check {
	for i := range checks {
		if checks[i].name == name {
			return &checks[i]
		}
	}
	return nil
}

// TestDoctorAgentDoesNotReadALinkOrANonRegularFile checks a settings file that is a symbolic link, or a directory. Each is
// a warning with its reason, and the file it names is not read: the link's target holds a Stop hook, which is then not
// found.
func TestDoctorAgentDoesNotReadALinkOrANonRegularFile(t *testing.T) {
	t.Run("a symbolic link", func(t *testing.T) {
		dir := agentProject(t)
		target := writeFile(t, dir, "elsewhere.json", stopEntry)
		path := settingsIn(dir)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("this system cannot make a symbolic link: %v", err)
		}

		checks := claudeCodeChecks(dir)
		assertSettingsWarning(t, checks, path+" is a symbolic link, which doctor does not follow"+tailRefused)
		if stop := checkNamed(checks, "Stop"); stop == nil || stop.status != "warn" {
			t.Errorf("Stop is %+v, want a warning: the link's target is not read", stop)
		}
	})
	t.Run("a directory", func(t *testing.T) {
		dir := agentProject(t)
		path := settingsIn(dir)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}

		assertSettingsWarning(t, claudeCodeChecks(dir), path+" is not a regular file"+tailRefused)
	})
}

// TestDoctorAgentFollowsTheUsersSettingsLink checks the user's settings file when it is a link, as a dotfiles repository
// makes it: the link is followed, and the hook in its target is found with no warning. A target that is not a regular file
// is still a warning, and the warning does not say enable refuses the file, since enable never writes the user's file.
func TestDoctorAgentFollowsTheUsersSettingsLink(t *testing.T) {
	t.Run("a link to a file with a Stop hook", func(t *testing.T) {
		dir := agentProject(t)
		target := writeFile(t, dir, "dotfiles-settings.json", stopEntry)
		user := userSettings(t)
		if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, user); err != nil {
			t.Skipf("this system cannot make a symbolic link: %v", err)
		}

		checks := claudeCodeChecks(dir)
		if stop := checkNamed(checks, "Stop"); stop == nil || stop.status != "ok" {
			t.Errorf("Stop is %+v, want it found through the link", stop)
		}
		for _, c := range checks {
			if c.name == "settings" {
				t.Errorf("a link the user owns is warned about: %+v", c)
			}
		}
	})
	t.Run("a link to a directory", func(t *testing.T) {
		dir := agentProject(t)
		target := filepath.Join(dir, "a-directory")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		user := userSettings(t)
		if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, user); err != nil {
			t.Skipf("this system cannot make a symbolic link: %v", err)
		}

		assertSettingsWarning(t, claudeCodeChecks(dir), user+" is not a regular file")
	})
}

// TestDoctorAgentNamesADanglingUsersSettingsLink checks the user's settings file when it is a link that leads to nothing: doctor
// gives one warning line, which names the dead link and its target, and reads no hook from it.
func TestDoctorAgentNamesADanglingUsersSettingsLink(t *testing.T) {
	dir := agentProject(t)
	user := userSettings(t)
	if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "no-such-settings.json")
	if err := os.Symlink(target, user); err != nil {
		t.Skipf("this system cannot make a symbolic link: %v", err)
	}

	checks := claudeCodeChecks(dir)
	var warnings []check
	for _, c := range checks {
		if c.name == "settings" {
			warnings = append(warnings, c)
		}
	}
	want := user + " is a symbolic link to " + target + ", which does not exist, so doctor reads no hooks from it"
	if len(warnings) != 1 || warnings[0].status != "warn" || warnings[0].detail != want {
		t.Errorf("settings checks are %+v, want one warning: %q", warnings, want)
	}
	if stop := checkNamed(checks, "Stop"); stop == nil || stop.status != "warn" {
		t.Errorf("Stop is %+v, want a warning that no hook is installed", stop)
	}
}

// TestDoctorAgentDoesNotReadAFileLargerThanTheCap checks the cap. A file of exactly maxSettingsBytes is read, with no
// warning. One byte more is a warning that says doctor does not read it, and enable is not named as refusing it.
func TestDoctorAgentDoesNotReadAFileLargerThanTheCap(t *testing.T) {
	dir := agentProject(t)
	local := settingsIn(dir)

	writeSettings(t, local, `{`+strings.Repeat(" ", maxSettingsBytes-2)+`}`)
	for _, c := range claudeCodeChecks(dir) {
		if c.name == "settings" {
			t.Errorf("a file at the cap is refused: %+v", c)
		}
	}

	writeSettings(t, local, `{`+strings.Repeat(" ", maxSettingsBytes-1)+`}`)
	assertSettingsWarning(t, claudeCodeChecks(dir), local+" is larger than 1048576 bytes, so doctor does not read it")
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
		{"an unknown agent", []string{"doctor", "--agent", "nope"}, "unknown agent"},
		{"an argument after the flags", []string{"doctor", "--agent", "claude-code", "extra"}, "takes flags only"},
		{"an unknown flag", []string{"doctor", "--nope"}, "run cairn doctor --help"},
		{"an agent with no value", []string{"doctor", "--agent"}, "run cairn doctor --help"},
		{"an empty agent", []string{"doctor", "--agent", ""}, "needs a name"},
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

// TestMatcherWithControlCharactersIsOneLine checks a matcher from a settings file, which a cloned repository may supply.
// A line feed in it would forge a line, and an escape sequence would move the terminal. The JSON escapes below decode to
// a line feed and an escape character. Doctor and enable each print one line per entry, with no escape character.
func TestMatcherWithControlCharactersIsOneLine(t *testing.T) {
	const hostile = `"matcher":"startup\nStop ok forged\u001b[2J"`
	const sessionStart = `{"hooks":{"SessionStart":[{` + hostile + `,"hooks":[{"type":"command","command":"cairn hook session-start"}]}]}}`
	assertOneLine := func(t *testing.T, stdout string) {
		t.Helper()
		if strings.Contains(stdout, "\x1b") {
			t.Errorf("output holds an escape character:\n%q", stdout)
		}
		if n := countLines(stdout, "SessionStart"); n != 1 {
			t.Errorf("%d SessionStart lines, want one:\n%s", n, stdout)
		}
		if n := countLines(stdout, "Stop"); n != 1 {
			t.Errorf("%d Stop lines, want one: a matcher forged a line:\n%s", n, stdout)
		}
		if !strings.Contains(stdout, "startup Stop ok forged") {
			t.Errorf("the matcher is not shown as one line:\n%s", stdout)
		}
	}

	t.Run("doctor", func(t *testing.T) {
		_, url := newServer(t, nil)
		dir := serverProject(t, url)
		writeSettings(t, settingsIn(dir), sessionStart)
		_, stdout, _ := runCmd(t, "doctor", "--agent", "claude-code")
		assertOneLine(t, stdout)
	})
	t.Run("enable", func(t *testing.T) {
		dir := enableProject(t)
		writeSettings(t, settingsIn(dir), sessionStart)
		code, stdout, stderr := runCmd(t, "enable", "--agent", "claude-code")
		if code != 0 || stderr != "" {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		assertOneLine(t, stdout)
	})
}

// countLines returns how many lines of s start with prefix.
func countLines(s, prefix string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}
	return n
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
		if code != 0 || stderr != "" || !strings.Contains(stdout, "Usage: cairn doctor [--agent claude-code|opencode|pi]") || !strings.Contains(stdout, "-agent string") {
			t.Errorf("doctor %s: exit %d, stdout %q, stderr %q", flag, code, stdout, stderr)
		}
	}
}
