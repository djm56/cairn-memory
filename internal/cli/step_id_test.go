package cli

import (
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// TestStepAddAfterIDSendsTheID checks that step add --after-id names the step to insert after by its id, and
// that the request carries an id ref, not a number ref (CONTRACT §1.4).
func TestStepAddAfterIDSendsTheID(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{step: &core.Step{ID: 21, Number: 3, Title: "t"}}
	code, _, errOut := runWrite(t, Step, svc, "", "add", "task-20261030-01", "--title", "t", "--after-id", "7", "--agent", "hicks")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0", code, errOut)
	}
	if svc.gotAdd.After == nil || svc.gotAdd.After.ID == nil || *svc.gotAdd.After.ID != 7 || svc.gotAdd.After.Number != nil {
		t.Errorf("after = %+v, want the id ref 7 and no number", svc.gotAdd.After)
	}
}

// TestStepAddAfterIDUsageErrors checks the refusals of --after-id: an id under 1 and a position given both ways.
// Each is a usage error, exit 2, before any call.
func TestStepAddAfterIDUsageErrors(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"id zero", []string{"--after-id", "0"}, "--after-id must be a step id, 1 or more"},
		{"id negative", []string{"--after-id", "-3"}, "--after-id must be a step id, 1 or more"},
		{"both forms", []string{"--after", "1", "--after-id", "7"}, "--after or by --after-id, not both"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeWrites{}
			args := append([]string{"add", "task-20261030-01", "--title", "t", "--agent", "hicks"}, c.args...)
			code, out, errOut := runWrite(t, Step, svc, "", args...)
			if code != 2 || out != "" || svc.calls != 0 || !strings.Contains(errOut, c.want) {
				t.Errorf("exit %d, stdout %q, stderr %q, calls %d; want exit 2, nothing on stdout, %q and no call",
					code, out, errOut, svc.calls, c.want)
			}
		})
	}
}

// TestTaskGetStepIDSendsTheID checks that task get --step-id names the step by its id in the request, and that
// it is not sent as a number.
func TestTaskGetStepIDSendsTheID(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeTasks{detail: &core.TaskDetail{Task: activeTask()}}
	code, _, errOut := runTask(t, svc, "get", "task-20261030-01", "--section", "events", "--step-id", "4")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0", code, errOut)
	}
	if ref := svc.gotGet.Step; ref == nil || ref.ID == nil || *ref.ID != 4 || ref.Number != nil {
		t.Errorf("the step ref was %+v, want the id 4 and no number", ref)
	}
}

// TestTaskGetStepIDUsageErrors checks the refusals of --step-id: an id under 1, and a step given both ways. Each
// is a usage error, exit 2, before any call.
func TestTaskGetStepIDUsageErrors(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"id zero", []string{"--step-id", "0"}, "--step-id must be a step id, 1 or more"},
		{"both forms", []string{"--step", "2", "--step-id", "4"}, "--step or by --step-id, not both"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeTasks{}
			args := append([]string{"get", "task-20261030-01"}, c.args...)
			code, out, errOut := runTask(t, svc, args...)
			if code != 2 || out != "" || svc.calls != 0 || !strings.Contains(errOut, c.want) {
				t.Errorf("exit %d, stdout %q, stderr %q, calls %d; want exit 2, nothing on stdout, %q and no call",
					code, out, errOut, svc.calls, c.want)
			}
		})
	}
}
