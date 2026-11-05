package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// roleSvc is a core.Service whose TaskCurrent records the caller of each call, so a test can see
// the agent role the call named. It answers with the TaskCurrent it is given.
type roleSvc struct {
	core.Unimplemented
	cur   *core.TaskCurrent
	gotAg string
}

// TaskCurrent records the agent of the caller and returns the answer set.
func (r *roleSvc) TaskCurrent(ctx context.Context) (*core.TaskCurrent, error) {
	c, _ := core.CallerFrom(ctx)
	r.gotAg = c.Agent
	return r.cur, nil
}

// TestRoleFromEnvIsTheVariableWhenItIsValid checks the agent role the counts are read for: the
// variable when it is set to a name the wire form carries. Unset and blank values are no role and
// are not refused. A value with a space at an end, or with a control character, is refused: no role,
// and the refusal is reported, so the block can name the variable.
func TestRoleFromEnvIsTheVariableWhenItIsValid(t *testing.T) {
	cases := []struct {
		name, value, want string
		refused           bool
	}{
		{"a name", "hicks", "hicks", false},
		{"unset", "", "", false},
		{"blank", "   ", "", false},
		{"a trailing space", "hicks ", "", true},
		{"a control character", "hicks" + string(rune(1)), "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("CAIRN_AGENT", c.value)
			got, refused := roleFromEnv()
			if got != c.want || refused != c.refused {
				t.Errorf("roleFromEnv() = (%q, %v), want (%q, %v)", got, refused, c.want, c.refused)
			}
		})
	}
}

// TestStatusDropsTheNoteWithARole checks that status keeps the no-role note when there is no role,
// and leaves it out when there is one, since the count then includes the role's own directives.
func TestStatusDropsTheNoteWithARole(t *testing.T) {
	var with bytes.Buffer
	writeStatus(&with, &target{role: "hicks"}, countsTask())
	if strings.Contains(with.String(), noRoleNote) {
		t.Errorf("status with a role still says no role is set:\n%s", with.String())
	}
	if !strings.Contains(with.String(), "directives  0 apply to this project") {
		t.Errorf("status with a role lost its directive count:\n%s", with.String())
	}
	var without bytes.Buffer
	writeStatus(&without, &target{}, countsTask())
	if !strings.Contains(without.String(), noRoleNote) {
		t.Errorf("status with no role does not say so:\n%s", without.String())
	}
}

// TestSessionStartDropsTheNoteWithARole checks the same for the session-start block: the no-role
// sentence is written with no role and left out with one.
func TestSessionStartDropsTheNoteWithARole(t *testing.T) {
	const sentence = "Agent-scoped directives were not counted: no agent role is set."
	var with bytes.Buffer
	writeSessionStart(&with, "acme-api", countsTask(), "hicks", false)
	if strings.Contains(with.String(), sentence) {
		t.Errorf("session start with a role still says no role is set:\n%s", with.String())
	}
	var without bytes.Buffer
	writeSessionStart(&without, "acme-api", countsTask(), "", false)
	if !strings.Contains(without.String(), sentence) {
		t.Errorf("session start with no role does not say so:\n%s", without.String())
	}
}

// TestCurrentForNamesTheRoleOnTheCall checks that status and the hook read the active task as the
// role: the call's caller carries it, so the server counts its agent-scoped directives. With no
// role the caller carries none.
func TestCurrentForNamesTheRoleOnTheCall(t *testing.T) {
	ctx := core.WithCaller(context.Background(), core.Caller{Project: "acme-api", Client: cliClient, Role: core.RoleAgent})
	svc := &roleSvc{cur: countsTask()}
	if _, err := currentFor(&target{svc: svc, ctx: ctx, role: "hicks"}); err != nil {
		t.Fatal(err)
	}
	if svc.gotAg != "hicks" {
		t.Errorf("the call ran for agent %q, want hicks", svc.gotAg)
	}
	if _, err := currentFor(&target{svc: svc, ctx: ctx}); err != nil {
		t.Fatal(err)
	}
	if svc.gotAg != "" {
		t.Errorf("with no role the call ran for agent %q, want none", svc.gotAg)
	}
}

// seedAgentDirective ratifies an agent-scoped directive for role, as the operator. An agent directive
// applies in every project, so it is counted by the status of any project that the role reads for.
func seedAgentDirective(t *testing.T, local *core.Local, role, title string) {
	t.Helper()
	ctx := core.WithCaller(context.Background(), core.Caller{Role: core.RoleOperator, Agent: "operator"})
	if _, err := local.DirectiveRatify(ctx, core.DirectiveRatifyRequest{
		Scope: core.ScopeAgent, Agent: role, Title: title, Rule: "Read the schema first.",
	}); err != nil {
		t.Fatalf("ratify agent directive for %s: %v", role, err)
	}
}

// TestStatusAndHookWithARoleFromTheVariable runs status and the session-start hook against a real
// server. Two agent-scoped directives are seeded, one for hicks and one for another role. With
// CAIRN_AGENT=hicks the status count is one higher than with the variable unset, so the server
// counted the role's own directive and not the other role's. A value the wire form refuses is no
// role: status and the hook name the variable as refused, and the run still exits 0.
func TestStatusAndHookWithARoleFromTheVariable(t *testing.T) {
	local, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	seedNamedTask(t, local, "acme-api", "Count with a role", "Read the schema first")
	seedAgentDirective(t, local, "hicks", "Hicks reads the schema first")
	seedAgentDirective(t, local, "reviewer", "Reviewer checks the diff")

	const unsetCount = "directives  0 apply to this project"
	const hicksCount = "directives  1 apply to this project"

	t.Setenv("CAIRN_AGENT", "")
	os.Unsetenv("CAIRN_AGENT")
	code, unset, errOut := runCmd(t, "status")
	if code != 0 || errOut != "" || !strings.Contains(unset, unsetCount) {
		t.Errorf("status with CAIRN_AGENT unset: exit %d, stderr %q, want %q, output:\n%s", code, errOut, unsetCount, unset)
	}

	t.Setenv("CAIRN_AGENT", "hicks")
	code, status, errOut := runCmd(t, "status")
	if code != 0 || errOut != "" || !strings.Contains(status, hicksCount) || strings.Contains(status, noRoleNote) {
		t.Errorf("status with CAIRN_AGENT=hicks: exit %d, stderr %q, want %q, output:\n%s", code, errOut, hicksCount, status)
	}
	code, hook, errOut := runCmd(t, "hook", "session-start")
	if code != 0 || errOut != "" || strings.Contains(hook, "no agent role is set") {
		t.Errorf("hook with CAIRN_AGENT=hicks: exit %d, stderr %q, output:\n%s", code, errOut, hook)
	}

	t.Setenv("CAIRN_AGENT", "hicks ")
	code, status, errOut = runCmd(t, "status")
	if code != 0 || errOut != "" || !strings.Contains(status, refusedRoleNote) || strings.Contains(status, noRoleNote) {
		t.Errorf("status with a refused CAIRN_AGENT: exit %d, stderr %q, want the refused note, output:\n%s", code, errOut, status)
	}
	code, hook, errOut = runCmd(t, "hook", "session-start")
	if code != 0 || errOut != "" || !strings.Contains(hook, refusedRoleSentence) || strings.Contains(hook, "no agent role is set") {
		t.Errorf("hook with a refused CAIRN_AGENT: exit %d, stderr %q, want the refused sentence, output:\n%s", code, errOut, hook)
	}
}
