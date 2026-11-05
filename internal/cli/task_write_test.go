package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// startedResult is the answer of task_start for a task with the two steps of twoSteps.
func startedResult() *core.TaskStartResult {
	return &core.TaskStartResult{ID: "task-20261030-01", Status: core.TaskActive, Steps: twoSteps()}
}

// TestTaskStartHuman checks the human output of task start: the id and status, then the plan,
// and that the request carries the brief and the agent the flag named.
func TestTaskStartHuman(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{start: startedResult()}
	code, out, errOut := runWrite(t, Task, svc, "", "start", "--agent", "hicks",
		"--title", "Add rate limit", "--goal", "Requests over 100/min get 429.")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	for _, want := range []string{
		"task-20261030-01  active\n",
		"steps\n  1. [active] Write test\n  2. [pending] Implement\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if svc.gotStart.Title != "Add rate limit" || svc.gotStart.Goal != "Requests over 100/min get 429." {
		t.Errorf("the request was %+v, want the title and the goal", svc.gotStart)
	}
	if got := agentOf(t, svc.ctx); got != "hicks" {
		t.Errorf("the call ran as %q, want hicks", got)
	}
}

// TestTaskStartJSON checks the --json output of task start: one line, decoding to the result of
// task_start with the new id.
func TestTaskStartJSON(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{start: startedResult()}
	code, out, errOut := runWrite(t, Task, svc, "", "start", "--agent", "hicks", "--title", "t", "--goal", "g", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	if !oneLine(out) {
		t.Fatalf("--json output is not one line: %q", out)
	}
	var got core.TaskStartResult
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.ID != "task-20261030-01" || len(got.Steps) != 2 {
		t.Errorf("--json output decodes to %+v (%v), want the started task", got, err)
	}
}

// TestTaskStartSendsTheJSONInput checks that --json-input carries the criteria, the key files and
// the plan with their assignees, from standard input.
func TestTaskStartSendsTheJSONInput(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{start: startedResult()}
	stdin := `{"criteria": ["429 after 100 requests", "limit configurable"],
		"key_files": [{"path": "internal/limit/limit.go", "note": "the limiter"}],
		"steps": [{"title": "Write test", "agent": "hicks"}, {"title": "Implement"}]}`
	code, _, errOut := runWrite(t, Task, svc, stdin, "start", "--agent", "hicks", "--title", "t", "--goal", "g", "--json-input")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	got := svc.gotStart
	if len(got.Criteria) != 2 || got.Criteria[1] != "limit configurable" {
		t.Errorf("criteria %q, want the two given", got.Criteria)
	}
	if len(got.KeyFiles) != 1 || got.KeyFiles[0].Path != "internal/limit/limit.go" || got.KeyFiles[0].Note != "the limiter" {
		t.Errorf("key files %+v, want the one given", got.KeyFiles)
	}
	if len(got.Steps) != 2 || got.Steps[0].Agent != "hicks" || got.Steps[1].Agent != "" {
		t.Errorf("steps %+v, want the two given with the assignee of the first", got.Steps)
	}
}

// TestTaskStartTextFromStandardInput checks that a text flag set to "-" carries standard input
// as its text, with the line breaks it has.
func TestTaskStartTextFromStandardInput(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{start: startedResult()}
	code, _, errOut := runWrite(t, Task, svc, "Requests over 100/min\nget 429.\n",
		"start", "--agent", "hicks", "--title", "t", "--goal", "-")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if svc.gotStart.Goal != "Requests over 100/min\nget 429." {
		t.Errorf("the goal is %q, want the input with its line breaks", svc.gotStart.Goal)
	}
}

// TestTaskStartPriorityAndQueue checks that --priority and --queue reach the request.
func TestTaskStartPriorityAndQueue(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{start: startedResult()}
	if code, _, errOut := runWrite(t, Task, svc, "", "start", "--agent", "hicks", "--title", "t", "--goal", "g", "--priority", "high", "--queue"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if svc.gotStart.Priority != core.PriorityHigh || !svc.gotStart.Queue {
		t.Errorf("the request was %+v, want high priority and queued", svc.gotStart)
	}
}

// TestTaskStartNeedsAnAgent checks the usage error of a write with no agent: it names the flag and
// the variable, and makes no call.
func TestTaskStartNeedsAnAgent(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{start: startedResult()}
	code, out, errOut := runWrite(t, Task, svc, "", "start", "--title", "t", "--goal", "g")
	if code != 2 || out != "" || svc.calls != 0 {
		t.Errorf("exit %d, stdout %q, calls %d; want a usage error and no call", code, out, svc.calls)
	}
	for _, want := range []string{"cairn task start: no agent name", "--agent", identity.EnvAgent} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr %q lacks %q", errOut, want)
		}
	}
}

// TestTaskStartAgentFromTheVariable checks that CAIRN_AGENT names the agent when --agent is left out.
func TestTaskStartAgentFromTheVariable(t *testing.T) {
	t.Setenv(identity.EnvAgent, "hicks")
	svc := &fakeWrites{start: startedResult()}
	if code, _, errOut := runWrite(t, Task, svc, "", "start", "--title", "t", "--goal", "g"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := agentOf(t, svc.ctx); got != "hicks" {
		t.Errorf("the call ran as %q, want hicks from %s", got, identity.EnvAgent)
	}
}

// TestTaskStartRefusedByTheServer checks a refusal of the server: its words on stderr, one line,
// nothing on stdout, exit 1.
func TestTaskStartRefusedByTheServer(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{err: core.Refused("another task is active in this project")}
	code, out, errOut := runWrite(t, Task, svc, "", "start", "--agent", "hicks", "--title", "t", "--goal", "g")
	if code != 1 || out != "" || errOut != "cairn task start: another task is active in this project\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 1, nothing on stdout and the refusal", code, out, errOut)
	}
}

// TestTaskStartUsageErrors checks the usage errors of task start: a task id it does not take, two
// flags that read standard input, a key --json-input does not name, and a blank agent. Each exits
// 2 and makes no call.
func TestTaskStartUsageErrors(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	cases := []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"a task id", "", []string{"start", "task-20261030-01", "--agent", "hicks"}, "takes no task id"},
		{"two flags read standard input", "text", []string{"start", "--agent", "hicks", "--goal", "-", "--context", "-"}, "both read standard input"},
		{"a key --json-input does not name", `{"critera": []}`, []string{"start", "--agent", "hicks", "--json-input"}, "not the JSON object this call takes"},
		{"a blank agent", "", []string{"start", "--agent", "  "}, "no agent name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeWrites{start: startedResult()}
			code, _, errOut := runWrite(t, Task, svc, c.stdin, c.args...)
			if code != 2 || !strings.Contains(errOut, c.want) || svc.calls != 0 {
				t.Errorf("exit %d, stderr %q, calls %d; want a usage error naming %q and no call", code, errOut, svc.calls, c.want)
			}
		})
	}
}

// closedTask is the answer of task_close for a task that finished done, with its summary.
func closedTask() *core.Task {
	task := activeTask()
	task.Status = core.TaskDone
	task.OutcomeSummary = "limit shipped"
	return &task
}

// TestTaskCloseHuman checks the human output of task close: the closed task with its outcome, and
// that the request carries the outcome and the summary, with no list, since none was given.
func TestTaskCloseHuman(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{task: closedTask()}
	code, out, errOut := runWrite(t, Task, svc, "", "close", "task-20261030-01", "--outcome", "done", "--summary", "limit shipped", "--agent", "hicks")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	for _, want := range []string{"task-20261030-01  done  normal  Add rate limit\n", "outcome     limit shipped\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	req := svc.gotClose
	if req.ID != "task-20261030-01" || req.Outcome != core.TaskDone || req.Summary != "limit shipped" {
		t.Errorf("the request was %+v, want the id, outcome done and the summary", req)
	}
	if req.Lessons != nil || req.Mistakes != nil || req.Criteria != nil {
		t.Errorf("lists were sent though none was given: lessons %v, mistakes %v, criteria %v", req.Lessons, req.Mistakes, req.Criteria)
	}
}

// TestTaskCloseJSONInputKeepsAbsentAndEmpty checks the rule of CONTRACT §2 through the input: a list
// given as [] reaches the request as empty, and a list left out reaches it as absent.
func TestTaskCloseJSONInputKeepsAbsentAndEmpty(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{task: closedTask()}
	stdin := `{"criteria": [{"ord": 1, "result": "met", "evidence": "test ok"}], "lessons": [],` +
		` "deliverables": [{"path": "docs/limit.md", "note": "write-up"}]}`
	code, _, errOut := runWrite(t, Task, svc, stdin, "close", "task-20261030-01", "--outcome", "done", "--summary", "s", "--json-input", "--agent", "hicks")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	req := svc.gotClose
	if len(req.Criteria) != 1 || req.Criteria[0].Result != core.CriterionMet || req.Criteria[0].Evidence != "test ok" {
		t.Errorf("criteria %+v, want the one result given", req.Criteria)
	}
	if req.Lessons == nil || len(req.Lessons) != 0 {
		t.Errorf("lessons %v, want an empty list, which says there are none", req.Lessons)
	}
	if req.Mistakes != nil {
		t.Errorf("mistakes %v, want absent: the key was left out", req.Mistakes)
	}
	if len(req.Deliverables) != 1 || req.Deliverables[0].Path != "docs/limit.md" {
		t.Errorf("deliverables %+v, want the one given", req.Deliverables)
	}
}

// TestTaskCloseNoLessonsReason checks that --no-lessons-reason reaches the request, for a close
// whose lessons are [].
func TestTaskCloseNoLessonsReason(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{task: closedTask()}
	if code, _, errOut := runWrite(t, Task, svc, "", "close", "task-20261030-01", "--outcome", "done", "--summary", "s", "--no-lessons-reason", "nothing new came up", "--agent", "hicks"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if svc.gotClose.NoLessonsReason != "nothing new came up" {
		t.Errorf("no-lessons reason %q, want the one given", svc.gotClose.NoLessonsReason)
	}
}

// TestTaskCloseRefusedByServer checks a refusal of the close policy: the server's message on stderr
// as one line, exit 1, and nothing on stdout.
func TestTaskCloseRefusedByServer(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	refusal := core.Refused("close policy not met: summary, lessons, mistakes")
	refusal.Missing = []string{core.MissingSummary, core.MissingLessons, core.MissingMistakes}
	svc := &fakeWrites{err: refusal}
	code, out, errOut := runWrite(t, Task, svc, "", "close", "task-20261030-01", "--outcome", "done", "--agent", "hicks")
	if code != 1 || out != "" || errOut != "cairn task close: close policy not met: summary, lessons, mistakes\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 1, nothing on stdout and the refusal", code, out, errOut)
	}
}

// TestTaskCloseUsageErrors checks the usage errors of task close: no task id, two flags that read
// standard input, a key --json-input does not name, and no agent. Each exits 2 and makes no call.
func TestTaskCloseUsageErrors(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	cases := []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"no task id", "", []string{"close", "--outcome", "done", "--summary", "s", "--agent", "hicks"}, "name one task id"},
		{"two flags read standard input", `{"lessons": []}`, []string{"close", "task-20261030-01", "--outcome", "done", "--summary", "-", "--json-input", "--agent", "hicks"}, "both read standard input"},
		{"a key --json-input does not name", `{"lesson": []}`, []string{"close", "task-20261030-01", "--outcome", "done", "--summary", "s", "--json-input", "--agent", "hicks"}, "not the JSON object this call takes"},
		{"no agent", "", []string{"close", "task-20261030-01", "--outcome", "done", "--summary", "s"}, "no agent name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := &fakeWrites{task: closedTask()}
			code, _, errOut := runWrite(t, Task, svc, c.stdin, c.args...)
			if code != 2 || !strings.Contains(errOut, c.want) || svc.calls != 0 {
				t.Errorf("exit %d, stderr %q, calls %d; want a usage error naming %q and no call", code, errOut, svc.calls, c.want)
			}
		})
	}
}

// TestTaskUpdateHuman checks the human output of task update: the task in its layout, with the
// field it set, and that the request sets that field and no other.
func TestTaskUpdateHuman(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	task := activeTask()
	svc := &fakeWrites{task: &task}
	code, out, errOut := runWrite(t, Task, svc, "", "update", "task-20261030-01", "--next-action", "write the test", "--agent", "hicks")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	if !strings.HasPrefix(out, "task-20261030-01  active  normal  Add rate limit\n") || !strings.Contains(out, "next        write the test\n") {
		t.Errorf("output %q, want the task with its next action", out)
	}
	req := svc.gotUpdate
	if req.NextAction == nil || *req.NextAction != "write the test" {
		t.Errorf("next action %v, want the one given", req.NextAction)
	}
	if req.Status != nil || req.Owner != nil || req.Title != nil || req.Priority != nil || req.Blockers != nil || req.Criteria != nil {
		t.Errorf("the request changed fields that were not given: %+v", req)
	}
}

// TestTaskUpdateStatusAndReason checks that a status and its reason reach the request, and that a
// flag given blank clears its field rather than leaving it out.
func TestTaskUpdateStatusAndReason(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	task := activeTask()
	svc := &fakeWrites{task: &task}
	if code, _, errOut := runWrite(t, Task, svc, "", "update", "task-20261030-01", "--status", "blocked", "--reason", "waiting on review", "--blockers", "", "--agent", "hicks"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	req := svc.gotUpdate
	if req.Status == nil || *req.Status != core.TaskBlocked || req.Reason != "waiting on review" {
		t.Errorf("the request was %+v, want blocked with the reason", req)
	}
	if req.Blockers == nil || *req.Blockers != "" {
		t.Errorf("blockers %v, want a blank value sent to clear it", req.Blockers)
	}
}

// TestTaskUpdatePriority checks that --priority reaches the request as a priority.
func TestTaskUpdatePriority(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	task := activeTask()
	svc := &fakeWrites{task: &task}
	if code, _, errOut := runWrite(t, Task, svc, "", "update", "task-20261030-01", "--priority", "urgent", "--agent", "hicks"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if svc.gotUpdate.Priority == nil || *svc.gotUpdate.Priority != core.PriorityUrgent {
		t.Errorf("priority %v, want urgent", svc.gotUpdate.Priority)
	}
}

// TestTaskUpdateBriefListsReplaceOrKeep checks the rule of CONTRACT §2 through the input: a list
// given as [] replaces the list with none, and a list left out is not sent, so the task keeps it.
func TestTaskUpdateBriefListsReplaceOrKeep(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	task := activeTask()
	svc := &fakeWrites{task: &task}
	if code, _, errOut := runWrite(t, Task, svc, `{"criteria": [], "key_files": [{"path": "internal/limit/limit.go"}]}`,
		"update", "task-20261030-01", "--json-input", "--agent", "hicks"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	req := svc.gotUpdate
	if req.Criteria == nil || len(*req.Criteria) != 0 {
		t.Errorf("criteria %v, want an empty list that replaces the list", req.Criteria)
	}
	if req.KeyFiles == nil || len(*req.KeyFiles) != 1 {
		t.Errorf("key files %v, want the one given", req.KeyFiles)
	}
	if code, _, errOut := runWrite(t, Task, svc, `{"key_files": []}`, "update", "task-20261030-01", "--json-input", "--agent", "hicks"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if svc.gotUpdate.Criteria != nil {
		t.Errorf("criteria %v were sent though the input left them out", svc.gotUpdate.Criteria)
	}
}

// TestTaskUpdateTextFromStandardInput checks that a text flag set to "-" carries standard input.
func TestTaskUpdateTextFromStandardInput(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	task := activeTask()
	svc := &fakeWrites{task: &task}
	if code, _, errOut := runWrite(t, Task, svc, "write the failing test\n", "update", "task-20261030-01", "--next-action", "-", "--agent", "hicks"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if svc.gotUpdate.NextAction == nil || *svc.gotUpdate.NextAction != "write the failing test" {
		t.Errorf("next action %v, want the input", svc.gotUpdate.NextAction)
	}
}

// TestTaskUpdateJSON checks the --json output of task update: one line, decoding to the task.
func TestTaskUpdateJSON(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	task := activeTask()
	svc := &fakeWrites{task: &task}
	code, out, errOut := runWrite(t, Task, svc, "", "update", "task-20261030-01", "--priority", "high", "--agent", "hicks", "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, output %q; want one line and exit 0", code, errOut, out)
	}
	var got core.Task
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.ID != task.ID {
		t.Errorf("--json decodes to %+v (%v), want task %s", got, err, task.ID)
	}
}

// TestTaskUpdateRefusedByServer checks a refusal of the server, such as a second active task: one
// line on stderr, exit 1.
func TestTaskUpdateRefusedByServer(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeWrites{err: core.Refused("another task is active in this project")}
	code, out, errOut := runWrite(t, Task, svc, "", "update", "task-20261030-01", "--status", "active", "--agent", "hicks")
	if code != 1 || out != "" || errOut != "cairn task update: another task is active in this project\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 1 and the refusal", code, out, errOut)
	}
}

// TestTaskUpdateUsageErrors checks the usage errors of task update: no task id, a key --json-input
// does not name, two flags that read standard input, and no agent. Each exits 2 and makes no call.
func TestTaskUpdateUsageErrors(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	cases := []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"no task id", "", []string{"update", "--status", "active", "--agent", "hicks"}, "name one task id"},
		{"a key --json-input does not name", `{"criterion": []}`, []string{"update", "task-20261030-01", "--json-input", "--agent", "hicks"}, "not the JSON object this call takes"},
		{"two flags read standard input", "text", []string{"update", "task-20261030-01", "--goal", "-", "--context", "-", "--agent", "hicks"}, "both read standard input"},
		{"no agent", "", []string{"update", "task-20261030-01", "--status", "active"}, "no agent name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task := activeTask()
			svc := &fakeWrites{task: &task}
			code, _, errOut := runWrite(t, Task, svc, c.stdin, c.args...)
			if code != 2 || !strings.Contains(errOut, c.want) || svc.calls != 0 {
				t.Errorf("exit %d, stderr %q, calls %d; want a usage error naming %q and no call", code, errOut, svc.calls, c.want)
			}
		})
	}
}

// TestTaskStartHelpNamesTheVariable checks that -h names the agent variable and the stdin forms,
// so the fallback and the "-" are written where a person reads the flags.
func TestTaskStartHelpNamesTheVariable(t *testing.T) {
	svc := &fakeWrites{}
	_, out, _ := runWrite(t, Task, svc, "", "start", "-h")
	for _, want := range []string{identity.EnvAgent, "-json-input", "reads standard input"} {
		if !strings.Contains(out, want) {
			t.Errorf("the help lacks %q:\n%s", want, out)
		}
	}
}
