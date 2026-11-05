package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// envVerb is one verb of the CLI with the arguments and standard input it needs to get past its own usage
// checks, so the only thing that can stop it before the project is opened is the check of CAIRN_AGENT.
type envVerb struct {
	name  string
	args  []string
	stdin string
	run   command
}

// envVerbs returns every verb of the CLI that reads its flags, with the arguments that make it otherwise valid.
// Each verb must refuse a malformed CAIRN_AGENT before it opens the project, reads included (CONTRACT §1.1).
func envVerbs() []envVerb {
	return []envVerb{
		{"task current", []string{"current"}, "", Task},
		{"task get", []string{"get", "task-20261030-01"}, "", Task},
		{"task list", []string{"list"}, "", Task},
		{"task start", []string{"start", "--title", "t", "--goal", "g", "--agent", "hicks"}, "", Task},
		{"task update", []string{"update", "task-20261030-01", "--status", "active", "--agent", "hicks"}, "", Task},
		{"task close", []string{"close", "task-20261030-01", "--outcome", "done", "--summary", "s", "--agent", "hicks"}, "", Task},
		{"step add", []string{"add", "task-20261030-01", "--title", "t", "--agent", "hicks"}, "", Step},
		{"step update", []string{"update", "task-20261030-01", "1", "--action", "start", "--agent", "hicks"}, "", Step},
		{"step plan", []string{"plan", "task-20261030-01", "--json-input", "--agent", "hicks"}, `{"steps": []}`, Step},
		{"event log", []string{"log", "task-20261030-01", "--kind", "note", "--text", "x", "--agent", "hicks"}, "", Event},
		{"learning add", []string{"add", "--kind", "finding", "--title", "t", "--body", "b", "--agent", "hicks"}, "", Learning},
		{"learning list", []string{"list"}, "", Learning},
		{"directive list", []string{"list"}, "", envDirective},
		{"recall", []string{"--goal", "g"}, "", Recall},
		{"search", []string{"--query", "x"}, "", Search},
		{"prime", nil, "", envPrime},
	}
}

// envDirective is Directive with the signature a command has, so it can sit in envVerbs.
func envDirective(open Opener, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	return Directive(open, args, stdout, stderr)
}

// envPrime is Prime with the signature a command has, so it can sit in envVerbs.
func envPrime(open Opener, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	return Prime(open, args, stdout, stderr)
}

// TestEnvAgentIsCheckedByEveryVerb checks that a CAIRN_AGENT the wire form refuses is a usage error, exit 2,
// on every verb, reads included: nothing is written to stdout, the message names the variable, and the
// project is never opened. The names are a control character, a space at the start and a space at the end.
func TestEnvAgentIsCheckedByEveryVerb(t *testing.T) {
	for _, bad := range []string{"ma\nin", " main", "main "} {
		for _, v := range envVerbs() {
			t.Run(v.name+" "+strings.ReplaceAll(strings.ReplaceAll(bad, "\n", `\n`), " ", "_"), func(t *testing.T) {
				t.Setenv(identity.EnvAgent, bad)
				opened := 0
				open := func() (Session, error) {
					opened++
					return Session{}, core.Invalid("the project should not be opened")
				}
				var out, errOut bytes.Buffer
				code := v.run(open, v.args, strings.NewReader(v.stdin), &out, &errOut)
				if code != ExitUsage || opened != 0 || out.Len() != 0 || !strings.Contains(errOut.String(), identity.EnvAgent) || !strings.Contains(errOut.String(), "agent name") {
					t.Errorf("exit %d, stdout %q, stderr %q, opened %d; want exit 2, nothing on stdout, the name refusal on stderr and no open",
						code, out.String(), errOut.String(), opened)
				}
			})
		}
	}
}

// TestBlankEnvAgentIsNoName checks that a CAIRN_AGENT of only spaces is no name: every verb goes on past the
// check and opens the project. The open here refuses on purpose, so the run stops there, and the message is
// not the variable's.
func TestBlankEnvAgentIsNoName(t *testing.T) {
	t.Setenv(identity.EnvAgent, "   ")
	for _, v := range envVerbs() {
		t.Run(v.name, func(t *testing.T) {
			opened := 0
			open := func() (Session, error) {
				opened++
				return Session{}, core.Invalid("stop after the check")
			}
			var out, errOut bytes.Buffer
			v.run(open, v.args, strings.NewReader(v.stdin), &out, &errOut)
			if opened != 1 || strings.Contains(errOut.String(), identity.EnvAgent) {
				t.Errorf("opened %d, stderr %q; want the project opened once and no CAIRN_AGENT message", opened, errOut.String())
			}
		})
	}
}
