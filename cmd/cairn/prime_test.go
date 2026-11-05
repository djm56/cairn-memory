package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestPrimeIsInTheHelp checks that help lists prime with its one-line summary, and that prime -h prints
// its flags and exits 0.
func TestPrimeIsInTheHelp(t *testing.T) {
	code, stdout, stderr := runCmd(t, "help")
	if code != 0 || stderr != "" {
		t.Fatalf("help: exit %d, stderr %q", code, stderr)
	}
	const line = "  prime      The session-start view for an agent with only a shell: prime [--agent role], with --json\n"
	if !strings.Contains(stdout, line) {
		t.Errorf("help does not list prime with its summary; want the line %q in:\n%s", line, stdout)
	}

	code, stdout, stderr = runCmd(t, "prime", "-h")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "-agent string") || !strings.Contains(stdout, "-json") {
		t.Errorf("prime -h: exit %d, stderr %q, output %q; want 0 and the flags on stdout", code, stderr, stdout)
	}
}

// TestPrimeWithNoTaskOverTheServer checks the real path with no active task: prime writes the one
// no-task line for the project and exits 0.
func TestPrimeWithNoTaskOverTheServer(t *testing.T) {
	_, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CAIRN_AGENT", "")
	os.Unsetenv("CAIRN_AGENT")

	code, stdout, stderr := runCmd(t, "prime")
	if code != 0 || stderr != "" || stdout != "Cairn: no task is active in acme-api.\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 0 and the one no-task line", code, stdout, stderr)
	}
}

// TestPrimeRoundTripWithARoleFromTheVariable runs prime against a real server. Two agent-scoped
// directives are seeded, one for hicks and one for another role. With CAIRN_AGENT=hicks the count
// includes hicks's own directive, in the block, the summary and the JSON, and the block closes with the
// pointer to the brief. With the variable unset the count is the unscoped one and the no-role sentence is
// written. A value the wire form refuses is a usage error, exit 2.
func TestPrimeRoundTripWithARoleFromTheVariable(t *testing.T) {
	local, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	seedNamedTask(t, local, "acme-api", "Count with a role", "Read the schema first")
	seedAgentDirective(t, local, "hicks", "Hicks reads the schema first")
	seedAgentDirective(t, local, "reviewer", "Reviewer checks the diff")

	const pointer = "The brief, steps and recent events follow.\n"

	t.Setenv("CAIRN_AGENT", "hicks")
	code, out, errOut := runCmd(t, "prime")
	if code != 0 || errOut != "" {
		t.Fatalf("prime with CAIRN_AGENT=hicks: exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{
		"Directives that apply: 1.\n",
		pointer,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prime with CAIRN_AGENT=hicks lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, noRoleNote) {
		t.Errorf("prime with a role still says no role is set:\n%s", out)
	}

	code, out, errOut = runCmd(t, "prime", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("prime --json with CAIRN_AGENT=hicks: exit %d, stderr %q", code, errOut)
	}
	var got struct {
		Project string           `json:"project"`
		Role    string           `json:"role"`
		Current core.TaskCurrent `json:"current"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("prime --json does not decode: %v\n%s", err, out)
	}
	if got.Project != "acme-api" || got.Role != "hicks" {
		t.Errorf("project %q, role %q; want acme-api and hicks", got.Project, got.Role)
	}
	if len(got.Current.Directives) != 1 || got.Current.Directives[0].Title != "Hicks reads the schema first" {
		t.Errorf("current directives = %+v, want only hicks's directive", got.Current.Directives)
	}

	t.Setenv("CAIRN_AGENT", "")
	os.Unsetenv("CAIRN_AGENT")
	code, out, errOut = runCmd(t, "prime")
	if code != 0 || errOut != "" || !strings.Contains(out, "Directives that apply: 0.\n") || !strings.Contains(out, noRoleNote+"\n") {
		t.Errorf("prime with the variable unset: exit %d, stderr %q, output:\n%s", code, errOut, out)
	}

	t.Setenv("CAIRN_AGENT", "hicks ")
	code, out, errOut = runCmd(t, "prime")
	if code != 2 || out != "" || !strings.Contains(errOut, "the name from CAIRN_AGENT is refused") {
		t.Errorf("prime with a refused CAIRN_AGENT: exit %d, stdout %q, stderr %q; want exit 2 naming the variable", code, out, errOut)
	}
}
