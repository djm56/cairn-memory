package main

import (
	"bytes"
	"context"
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
// variable when it is set to a name the wire form carries, and no role otherwise. A blank value, a
// value with a space at an end, and one with a control character are no role.
func TestRoleFromEnvIsTheVariableWhenItIsValid(t *testing.T) {
	cases := []struct {
		name, value, want string
	}{
		{"a name", "hicks", "hicks"},
		{"unset", "", ""},
		{"blank", "   ", ""},
		{"a trailing space", "hicks ", ""},
		{"a control character", "hicks" + string(rune(1)), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("CAIRN_AGENT", c.value)
			if got := roleFromEnv(); got != c.want {
				t.Errorf("roleFromEnv() = %q, want %q", got, c.want)
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
	writeSessionStart(&with, "acme-api", countsTask(), "hicks")
	if strings.Contains(with.String(), sentence) {
		t.Errorf("session start with a role still says no role is set:\n%s", with.String())
	}
	var without bytes.Buffer
	writeSessionStart(&without, "acme-api", countsTask(), "")
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

// TestStatusAndHookWithARoleFromTheVariable runs status and the session-start hook against a real
// server with CAIRN_AGENT set, and checks that neither prints the no-role sentence. With an invalid
// value, the sentence is printed, as it is with no value.
func TestStatusAndHookWithARoleFromTheVariable(t *testing.T) {
	local, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	seedNamedTask(t, local, "acme-api", "Count with a role", "Read the schema first")

	t.Setenv("CAIRN_AGENT", "hicks")
	code, status, errOut := runCmd(t, "status")
	if code != 0 || errOut != "" || strings.Contains(status, noRoleNote) {
		t.Errorf("status with CAIRN_AGENT=hicks: exit %d, stderr %q, output:\n%s", code, errOut, status)
	}
	code, hook, errOut := runCmd(t, "hook", "session-start")
	if code != 0 || errOut != "" || strings.Contains(hook, "no agent role is set") {
		t.Errorf("hook with CAIRN_AGENT=hicks: exit %d, stderr %q, output:\n%s", code, errOut, hook)
	}

	t.Setenv("CAIRN_AGENT", "hicks ")
	_, status, _ = runCmd(t, "status")
	if !strings.Contains(status, noRoleNote) {
		t.Errorf("status with an invalid CAIRN_AGENT does not say no role is set:\n%s", status)
	}
}
