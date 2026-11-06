package cli

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// fakeDirectives is a core.Service whose directive list answers what a test sets. It records the
// request of each call, and a test sets err to have every call refused.
type fakeDirectives struct {
	core.Unimplemented
	err     error
	list    *core.DirectiveList
	gotList core.DirectiveListRequest
	calls   int
}

func (f *fakeDirectives) DirectiveList(_ context.Context, req core.DirectiveListRequest) (*core.DirectiveList, error) {
	f.calls++
	f.gotList = req
	return f.list, f.err
}

// directiveRun runs Directive with svc behind it, and returns its exit code, stdout, stderr and the
// number of opens.
func directiveRun(svc core.Service, args ...string) (int, string, string, int) {
	return counted(svc, func(open Opener, stdout, stderr io.Writer) int {
		return Directive(open, args, stdout, stderr)
	})
}

// sampleDirectives is a list as the server gives it: one agent-scoped directive, then one global one,
// with two left out as expired and three as beyond the cap.
func sampleDirectives() *core.DirectiveList {
	return &core.DirectiveList{
		Directives: []core.Directive{
			{ID: 2, Code: "D-0002", Scope: core.ScopeAgent, Agent: "hicks", Title: "Hicks reads first", Rule: "Read the schema first.", Status: core.DirectiveActive},
			{ID: 1, Code: "D-0001", Scope: core.ScopeGlobal, Title: "Global rule", Rule: "Be exact.", Status: core.DirectiveActive},
		},
		Expired: 2,
		Omitted: 3,
	}
}

// TestDirectiveListHuman checks the summary of directive list: each directive's code, scope and title,
// its rule on the next line, and the expired and omitted counts when they are not zero.
func TestDirectiveListHuman(t *testing.T) {
	svc := &fakeDirectives{list: sampleDirectives()}
	code, out, errOut, _ := directiveRun(svc, "list")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	pad := strings.Repeat(" ", 10)
	want := "  D-0002  agent hicks  Hicks reads first\n" +
		pad + "Read the schema first.\n" +
		"  D-0001  global  Global rule\n" +
		pad + "Be exact.\n" +
		"expired     2 past review_by, left out\n" +
		"omitted     3 beyond the cap of 12\n"
	if out != want {
		t.Errorf("human output = %q, want %q", out, want)
	}
}

// TestDirectiveListCapsLongText checks that a title and a rule longer than one line are cut as Plain
// cuts them, the cap that recall's other lines have. The cut keeps the first MaxLineRunes-1 runes and ends
// in an ellipsis, so one directive cannot fill a context, and the summary stays one line per field.
func TestDirectiveListCapsLongText(t *testing.T) {
	long := strings.Repeat("x", MaxLineRunes+50)
	capped := strings.Repeat("x", MaxLineRunes-1) + "…"
	svc := &fakeDirectives{list: &core.DirectiveList{Directives: []core.Directive{
		{ID: 1, Code: "D-0001", Scope: core.ScopeGlobal, Title: long, Rule: long, Status: core.DirectiveActive},
	}}}
	code, out, errOut, _ := directiveRun(svc, "list")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	pad := strings.Repeat(" ", 10)
	want := "  D-0001  global  " + capped + "\n" + pad + capped + "\n"
	if out != want {
		t.Errorf("human output = %q, want the title and rule cut to %d runes", out, MaxLineRunes)
	}
}

// TestDirectiveListEmpty checks that a list with no directive says so, and still prints its counts.
func TestDirectiveListEmpty(t *testing.T) {
	svc := &fakeDirectives{list: &core.DirectiveList{Directives: []core.Directive{}}}
	code, out, errOut, _ := directiveRun(svc, "list")
	if code != 0 || errOut != "" || out != "no directives\n" {
		t.Errorf("exit %d, stderr %q, output %q; want the no-directive line", code, errOut, out)
	}
}

// TestDirectiveListJSON checks the --json output of directive list: one line, the object the MCP tool
// returns, decoding to the directives and the two counts.
func TestDirectiveListJSON(t *testing.T) {
	svc := &fakeDirectives{list: sampleDirectives()}
	code, out, errOut, _ := directiveRun(svc, "list", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, want one line of JSON, got %q", code, errOut, out)
	}
	var got core.DirectiveList
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the output is not a directive list: %v\n%s", err, out)
	}
	if len(got.Directives) != 2 || got.Directives[0].Code != "D-0002" || got.Expired != 2 || got.Omitted != 3 {
		t.Errorf("decoded list = %+v, want the directives and counts the server gave", got)
	}
}

// TestDirectiveListEmptyJSON checks that a list with no directive writes the directives as [], an
// array, and the counts as 0, as the contract has them.
func TestDirectiveListEmptyJSON(t *testing.T) {
	svc := &fakeDirectives{list: &core.DirectiveList{Directives: []core.Directive{}}}
	code, out, errOut, _ := directiveRun(svc, "list", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, want one line of JSON, got %q", code, errOut, out)
	}
	if got := jsonField(t, out, "directives"); got != "[]" {
		t.Errorf("directives = %s, want []", got)
	}
	if expired, omitted := jsonField(t, out, "expired"), jsonField(t, out, "omitted"); expired != "0" || omitted != "0" {
		t.Errorf("expired %s, omitted %s; want 0 and 0", expired, omitted)
	}
}

// TestDirectiveListAgentFlag checks the request of directive list. --agent narrows the list to the
// role it names. With the flag left out, the request names no role, even when CAIRN_AGENT is set, so
// the list is every role, as the contract has it.
func TestDirectiveListAgentFlag(t *testing.T) {
	svc := &fakeDirectives{list: sampleDirectives()}
	if code, _, errOut, _ := directiveRun(svc, "list", "--agent", "reviewer"); code != 0 || errOut != "" || svc.gotList.Agent != "reviewer" {
		t.Errorf("exit %d, stderr %q, agent %q; want the role named", code, errOut, svc.gotList.Agent)
	}
	t.Setenv(identity.EnvAgent, "hicks")
	if code, _, errOut, _ := directiveRun(svc, "list"); code != 0 || errOut != "" || svc.gotList.Agent != "" {
		t.Errorf("with %s set: exit %d, stderr %q, agent %q; want no role, so every role is listed", identity.EnvAgent, code, errOut, svc.gotList.Agent)
	}
}

// TestDirectiveListRefusedThroughTheFake checks that a refusal of the server exits 1 with the reason
// on stderr and nothing on stdout.
func TestDirectiveListRefusedThroughTheFake(t *testing.T) {
	svc := &fakeDirectives{err: core.Refused("the directives could not be read")}
	code, out, errOut, _ := directiveRun(svc, "list")
	if code != 5 || out != "" || errOut != "cairn directive list: the directives could not be read\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 5 (refused) with the refusal on stderr", code, out, errOut)
	}
}

// TestDirectiveListUsageErrors checks the usage errors of directive list. Each exits 2 before the
// project is opened, and makes no call.
func TestDirectiveListUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"a blank agent", []string{"list", "--agent", " "}, "--agent must name a role"},
		{"an agent with an edge space", []string{"list", "--agent", "hicks "}, "the name from --agent is refused"},
		{"a positional argument", []string{"list", "hicks"}, "takes no positional argument"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeDirectives{}
			code, out, errOut, opened := directiveRun(svc, c.args...)
			if code != 2 || out != "" || !strings.Contains(errOut, c.want) || opened != 0 || svc.calls != 0 {
				t.Errorf("exit %d, stdout %q, stderr %q, opens %d, calls %d; want a usage error naming %q with no open or call",
					code, out, errOut, opened, svc.calls, c.want)
			}
		})
	}
}

// TestDirectiveNeedsAVerb checks that directive with no verb, or an unknown one, is a usage error that
// names the verb.
func TestDirectiveNeedsAVerb(t *testing.T) {
	svc := &fakeDirectives{}
	if code, _, errOut, opened := directiveRun(svc); code != 2 || !strings.Contains(errOut, "cairn directive list") || opened != 0 {
		t.Errorf("no verb: exit %d, stderr %q; want a usage error naming list", code, errOut)
	}
	if code, _, errOut, opened := directiveRun(svc, "add"); code != 2 || !strings.Contains(errOut, `unknown verb "add"`) || opened != 0 {
		t.Errorf("unknown verb: exit %d, stderr %q; want a usage error", code, errOut)
	}
}
