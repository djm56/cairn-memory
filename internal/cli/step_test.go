package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// TestStepAddHuman checks the human output of step add: the new step with its number, status and
// assignee, and that the request carries the position, the title, the assignee and the caller.
func TestStepAddHuman(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{step: &core.Step{ID: 43, Number: 2, Title: "Review", Status: core.StepPending, Agent: "reviewer"}}
	code, out, errOut := runWrite(t, Step, svc, "", "add", "task-20261030-01", "--title", "Review", "--after", "1", "--assignee", "reviewer", "--agent", "hicks")
	if code != 0 || errOut != "" || out != "2. [pending] Review (agent reviewer)\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want 0 and the step line", code, out, errOut)
	}
	got := svc.gotAdd
	if got.TaskID != "task-20261030-01" || got.Title != "Review" || got.Agent != "reviewer" || got.After == nil || got.After.Number == nil || *got.After.Number != 1 {
		t.Errorf("the request was %+v, want task, title, assignee and after 1", got)
	}
	if agentOf(t, svc.ctx) != "hicks" {
		t.Errorf("the call ran as %q, want hicks", agentOf(t, svc.ctx))
	}
}

// TestStepAddPositionFirstAndLast checks the position: --after 0 is the first place, a step ref
// that names no step; left out, the step goes last and no ref is sent.
func TestStepAddPositionFirstAndLast(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{step: &core.Step{Number: 1, Title: "Read", Status: core.StepPending}}
	if code, _, errOut := runWrite(t, Step, svc, "", "add", "task-20261030-01", "--title", "Read", "--after", "0", "--agent", "hicks"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if svc.gotAdd.After == nil || svc.gotAdd.After.Number == nil || *svc.gotAdd.After.Number != 0 {
		t.Errorf("--after 0: the ref was %+v, want number 0", svc.gotAdd.After)
	}
	if code, _, errOut := runWrite(t, Step, svc, "", "add", "task-20261030-01", "--title", "Last", "--agent", "hicks"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if svc.gotAdd.After != nil {
		t.Errorf("left out: the ref was %+v, want none, so the step goes last", svc.gotAdd.After)
	}
}

// TestStepAddJSON checks the --json output of step add: one line, decoding to the step.
func TestStepAddJSON(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{step: &core.Step{ID: 43, Number: 2, Title: "Review", Status: core.StepPending}}
	code, out, errOut := runWrite(t, Step, svc, "", "add", "task-20261030-01", "--title", "Review", "--agent", "hicks", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, output %q; want one line and exit 0", code, errOut, out)
	}
	var got core.Step
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.ID != 43 {
		t.Errorf("--json decodes to %+v (%v), want step 43", got, err)
	}
}

// TestStepAddRefusedByServer checks a refusal of the server: one line on stderr, exit 1.
func TestStepAddRefusedByServer(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{err: core.Refused("the task is done: no step can be added")}
	code, out, errOut := runWrite(t, Step, svc, "", "add", "task-20261030-01", "--title", "Review", "--agent", "hicks")
	if code != 1 || out != "" || errOut != "cairn step add: the task is done: no step can be added\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 1 and the refusal", code, out, errOut)
	}
}

// TestStepAddUsageErrors checks the usage errors of step add: no task id, a negative --after, a
// missing agent, and two flags that read standard input. Each exits 2 and makes no call.
func TestStepAddUsageErrors(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	cases := []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"no task id", "", []string{"add", "--title", "Review", "--agent", "hicks"}, "name one task id"},
		{"a negative after", "", []string{"add", "task-20261030-01", "--title", "R", "--after", "-1", "--agent", "hicks"}, "--after must be a step number"},
		{"no agent", "", []string{"add", "task-20261030-01", "--title", "R"}, "no agent name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeWrites{step: &core.Step{}}
			code, _, errOut := runWrite(t, Step, svc, c.stdin, c.args...)
			if code != 2 || !strings.Contains(errOut, c.want) || svc.calls != 0 {
				t.Errorf("exit %d, stderr %q, calls %d; want a usage error naming %q and no call", code, errOut, svc.calls, c.want)
			}
		})
	}
}

// TestStepUpdateHuman checks the human output of step update: the step with its new status and its
// summary, and that the request names the step by its number and the action.
func TestStepUpdateHuman(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{step: &core.Step{Number: 1, Title: "Write test", Status: core.StepDone, Summary: "passes"}}
	code, out, errOut := runWrite(t, Step, svc, "", "update", "task-20261030-01", "1", "--action", "finish", "--summary", "passes", "--agent", "hicks")
	if code != 0 || errOut != "" || out != "1. [done] Write test: passes\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want the step line", code, out, errOut)
	}
	got := svc.gotStepUpd
	if got.TaskID != "task-20261030-01" || got.Step.Number == nil || *got.Step.Number != 1 || got.Action != core.StepFinish || got.Summary != "passes" || got.Agent != nil {
		t.Errorf("the request was %+v, want step 1 finished with the summary and no reassignment", got)
	}
}

// TestStepUpdateAssignee checks that --assignee reassigns the step, and that a step with no
// --assignee keeps its assignee.
func TestStepUpdateAssignee(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{step: &core.Step{Number: 2, Title: "Review", Status: core.StepPending}}
	if code, _, errOut := runWrite(t, Step, svc, "", "update", "task-20261030-01", "2", "--action", "start", "--assignee", "reviewer", "--agent", "hicks"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if svc.gotStepUpd.Agent == nil || *svc.gotStepUpd.Agent != "reviewer" {
		t.Errorf("assignee %v, want reviewer", svc.gotStepUpd.Agent)
	}
	if code, _, errOut := runWrite(t, Step, svc, "", "update", "task-20261030-01", "2", "--action", "start", "--agent", "hicks"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if svc.gotStepUpd.Agent != nil {
		t.Errorf("no --assignee: the assignee was sent as %q, want none", *svc.gotStepUpd.Agent)
	}
}

// TestStepUpdateSummaryFromStandardInput checks that --summary - carries standard input.
func TestStepUpdateSummaryFromStandardInput(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{step: &core.Step{Number: 1, Status: core.StepFailed}}
	code, _, errOut := runWrite(t, Step, svc, "the build broke\n", "update", "task-20261030-01", "1", "--action", "fail", "--summary", "-", "--agent", "hicks")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if svc.gotStepUpd.Summary != "the build broke" {
		t.Errorf("the summary is %q, want the input without its line break", svc.gotStepUpd.Summary)
	}
}

// TestStepUpdateRefusedByServer checks a refusal of the server for a transition that is not allowed.
func TestStepUpdateRefusedByServer(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{err: core.Refused("start needs an active task")}
	code, out, errOut := runWrite(t, Step, svc, "", "update", "task-20261030-01", "1", "--action", "start", "--agent", "hicks")
	if code != 1 || out != "" || errOut != "cairn step update: start needs an active task\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 1 and the refusal", code, out, errOut)
	}
}

// TestStepUpdateUsageErrors checks the usage errors of step update: the step must be a number of
// 1 or more, a task id and a step are both needed, and an agent is needed. Each exits 2 and makes
// no call.
func TestStepUpdateUsageErrors(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"a word for the step", []string{"update", "task-20261030-01", "two", "--action", "start", "--agent", "hicks"}, "the step is a number"},
		{"step zero", []string{"update", "task-20261030-01", "0", "--action", "start", "--agent", "hicks"}, "the step is a number"},
		{"no step", []string{"update", "task-20261030-01", "--action", "start", "--agent", "hicks"}, "name a task id and a step number"},
		{"no agent", []string{"update", "task-20261030-01", "1", "--action", "start"}, "no agent name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeWrites{step: &core.Step{}}
			code, _, errOut := runWrite(t, Step, svc, "", c.args...)
			if code != 2 || !strings.Contains(errOut, c.want) || svc.calls != 0 {
				t.Errorf("exit %d, stderr %q, calls %d; want a usage error naming %q and no call", code, errOut, svc.calls, c.want)
			}
		})
	}
}

// TestStepPlanHuman checks the human output of step plan: the new plan as the steps list shows it,
// and that the request carries the steps with their assignees, read from standard input.
func TestStepPlanHuman(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{list: &core.StepList{Steps: twoSteps()}}
	stdin := `{"steps": [{"title": "Write test", "agent": "hicks"}, {"title": "Implement"}]}`
	code, out, errOut := runWrite(t, Step, svc, stdin, "plan", "task-20261030-01", "--json-input", "--agent", "hicks")
	if code != 0 || errOut != "" || out != "steps\n  1. [active] Write test\n  2. [pending] Implement\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q; want the plan", code, out, errOut)
	}
	got := svc.gotPlan
	if got.TaskID != "task-20261030-01" || len(got.Steps) != 2 || got.Steps[0].Agent != "hicks" || got.Steps[1].Agent != "" {
		t.Errorf("the request was %+v, want the two steps with the first's assignee", got)
	}
}

// TestStepPlanEmptyListRemovesPendingSteps checks that [] is sent as an empty plan, and that the
// answer for a plan with no steps is said in words.
func TestStepPlanEmptyListRemovesPendingSteps(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{list: &core.StepList{}}
	code, out, errOut := runWrite(t, Step, svc, `{"steps": []}`, "plan", "task-20261030-01", "--json-input", "--agent", "hicks")
	if code != 0 || errOut != "" || out != "no steps\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 0 and no steps", code, out, errOut)
	}
	if svc.gotPlan.Steps == nil || len(svc.gotPlan.Steps) != 0 {
		t.Errorf("steps %v, want an empty list sent", svc.gotPlan.Steps)
	}
}

// TestStepPlanJSON checks the --json output of step plan: one line, decoding to the step list.
func TestStepPlanJSON(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{list: &core.StepList{Steps: twoSteps()}}
	code, out, errOut := runWrite(t, Step, svc, `{"steps": [{"title": "Write test"}]}`, "plan", "task-20261030-01", "--json-input", "--agent", "hicks", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, output %q; want one line and exit 0", code, errOut, out)
	}
	var got core.StepList
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Steps) != 2 {
		t.Errorf("--json decodes to %+v (%v), want the two steps", got, err)
	}
}

// TestStepPlanRefusedByServer checks the refusal of a pending step that an event refers to: the
// server's words on stderr, exit 1.
func TestStepPlanRefusedByServer(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{err: core.Refused("step 2 has a review: skip it first")}
	code, out, errOut := runWrite(t, Step, svc, `{"steps": []}`, "plan", "task-20261030-01", "--json-input", "--agent", "hicks")
	if code != 1 || out != "" || errOut != "cairn step plan: step 2 has a review: skip it first\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 1 and the refusal", code, out, errOut)
	}
}

// TestStepPlanUsageErrors checks the usage errors of step plan: the steps must come from standard
// input with --json-input, the steps list must be present, and a task id is needed. Each exits 2
// and makes no call.
func TestStepPlanUsageErrors(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	cases := []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"no json-input", `{"steps": []}`, []string{"plan", "task-20261030-01", "--agent", "hicks"}, "give --json-input"},
		{"no steps list", `{}`, []string{"plan", "task-20261030-01", "--json-input", "--agent", "hicks"}, "no steps list"},
		{"a key it does not name", `{"step": []}`, []string{"plan", "task-20261030-01", "--json-input", "--agent", "hicks"}, "not the JSON object this call takes"},
		{"no task id", `{"steps": []}`, []string{"plan", "--json-input", "--agent", "hicks"}, "name one task id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeWrites{list: &core.StepList{}}
			code, _, errOut := runWrite(t, Step, svc, c.stdin, c.args...)
			if code != 2 || !strings.Contains(errOut, c.want) || svc.calls != 0 {
				t.Errorf("exit %d, stderr %q, calls %d; want a usage error naming %q and no call", code, errOut, svc.calls, c.want)
			}
		})
	}
}

// TestStepVerbs checks the usage errors of the command itself: no verb, and a verb it does not
// have.
func TestStepVerbs(t *testing.T) {
	svc := &fakeWrites{}
	if code, _, errOut := runWrite(t, Step, svc, ""); code != 2 || !strings.Contains(errOut, "cairn step add|update") {
		t.Errorf("no verb: exit %d, stderr %q", code, errOut)
	}
	if code, _, errOut := runWrite(t, Step, svc, "", "frobnicate"); code != 2 || !strings.Contains(errOut, `unknown verb "frobnicate"`) {
		t.Errorf("an unknown verb: exit %d, stderr %q", code, errOut)
	}
}
