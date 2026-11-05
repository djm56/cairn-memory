package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
)

// fakeTasks is a core.Service whose task reads answer what a test sets. It records the
// context and the request of each read, so a test can check what the command sent.
type fakeTasks struct {
	core.Unimplemented
	cur     *core.TaskCurrent
	detail  *core.TaskDetail
	list    *core.TaskList
	err     error
	gotCtx  context.Context
	gotGet  core.TaskGetRequest
	gotList core.TaskListRequest
	calls   int
}

func (f *fakeTasks) TaskCurrent(ctx context.Context) (*core.TaskCurrent, error) {
	f.calls++
	f.gotCtx = ctx
	return f.cur, f.err
}

func (f *fakeTasks) TaskGet(_ context.Context, req core.TaskGetRequest) (*core.TaskDetail, error) {
	f.calls++
	f.gotGet = req
	return f.detail, f.err
}

func (f *fakeTasks) TaskList(_ context.Context, req core.TaskListRequest) (*core.TaskList, error) {
	f.calls++
	f.gotList = req
	return f.list, f.err
}

// testCaller is the caller the open of every test runs as: the project and client the CLI
// gives the server, with the reader's role.
var testCaller = core.Caller{Project: "acme-api", Client: "cairn-cli", Role: core.RoleAgent}

// runTask runs the task command with svc behind it, and returns its exit code and what it
// wrote to stdout and stderr.
func runTask(t *testing.T, svc core.Service, args ...string) (int, string, string) {
	t.Helper()
	open := func() (Session, error) {
		return Session{Svc: svc, Ctx: core.WithCaller(context.Background(), testCaller)}, nil
	}
	var out, errOut bytes.Buffer
	code := Task(open, args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// oneLine reports whether s is exactly one line of text, with its newline at the end.
func oneLine(s string) bool {
	return strings.Count(s, "\n") == 1 && strings.HasSuffix(s, "\n")
}

// activeTask is a task with two steps, the first active, for the reads below.
func activeTask() core.Task {
	return core.Task{
		ID: "task-20261030-01", Project: "acme-api", Title: "Add rate limit",
		Goal: "Requests over 100/min get 429.", Status: core.TaskActive, Priority: core.PriorityNormal,
		NextAction: "write the test", OpenedAt: time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 10, 30, 9, 5, 0, 0, time.UTC),
	}
}

func twoSteps() []core.Step {
	return []core.Step{
		{ID: 41, Number: 1, Title: "Write test", Status: core.StepActive, Attempts: 1},
		{ID: 42, Number: 2, Title: "Implement", Status: core.StepPending},
	}
}

// TestTaskCurrentHuman checks the human output of task current: the task, its next action,
// the step to work on, and the directive count.
func TestTaskCurrentHuman(t *testing.T) {
	task := activeTask()
	svc := &fakeTasks{cur: &core.TaskCurrent{Task: &task, Steps: twoSteps()}}
	code, out, errOut := runTask(t, svc, "current")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	for _, want := range []string{
		"task        task-20261030-01  active  Add rate limit\n",
		"next        write the test\n",
		"step        1 of 2: Write test (active)\n",
		"directives  0 apply to this project\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// TestTaskCurrentJSON checks the --json output of task current: one line of the object the
// MCP tool returns, which decodes into the core type with the same task id.
func TestTaskCurrentJSON(t *testing.T) {
	task := activeTask()
	svc := &fakeTasks{cur: &core.TaskCurrent{Task: &task, Steps: twoSteps()}}
	code, out, errOut := runTask(t, svc, "current", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	if !oneLine(out) {
		t.Fatalf("--json output is not one line: %q", out)
	}
	var got core.TaskCurrent
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("--json output does not decode: %v: %s", err, out)
	}
	if got.Task == nil || got.Task.ID != task.ID || len(got.Steps) != 2 {
		t.Errorf("--json output decodes to %+v, want task %s and two steps", got, task.ID)
	}
}

// TestTaskCurrentWithNoTaskSaysSo checks that no active task is an answer, not a failure:
// the human line says so and the exit is 0.
func TestTaskCurrentWithNoTaskSaysSo(t *testing.T) {
	svc := &fakeTasks{cur: &core.TaskCurrent{}}
	code, out, errOut := runTask(t, svc, "current")
	if code != 0 || errOut != "" || out != "no task is active\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 0, %q and nothing on stderr", code, out, errOut, "no task is active\n")
	}
}

// TestTaskCurrentPassesTheAgentRole checks that --agent sets the caller's role, which names
// the agent-scoped directives, and leaves the project as it was.
func TestTaskCurrentPassesTheAgentRole(t *testing.T) {
	svc := &fakeTasks{cur: &core.TaskCurrent{}}
	if code, _, errOut := runTask(t, svc, "current", "--agent", "reviewer"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	c, ok := core.CallerFrom(svc.gotCtx)
	if !ok || c.Agent != "reviewer" || c.Project != "acme-api" {
		t.Errorf("the caller the read ran as is %+v, want role reviewer in project acme-api", c)
	}
}

// TestTaskCurrentError checks the error path: a failed call is one line on stderr with the
// command's name and the server's words, nothing on stdout, and exit 1.
func TestTaskCurrentError(t *testing.T) {
	svc := &fakeTasks{err: core.Invalid("the project has no row yet")}
	code, out, errOut := runTask(t, svc, "current")
	if code != 1 || out != "" {
		t.Errorf("exit %d, stdout %q; want 1 and nothing on stdout", code, out)
	}
	if want := "cairn task current: the project has no row yet\n"; errOut != want {
		t.Errorf("stderr %q, want %q", errOut, want)
	}
}

// TestTaskCurrentRefusesAnID checks the usage error: current takes no task id.
func TestTaskCurrentRefusesAnID(t *testing.T) {
	svc := &fakeTasks{}
	code, _, errOut := runTask(t, svc, "current", "task-20261030-01")
	if code != 2 || !strings.Contains(errOut, "takes no task id") || svc.calls != 0 {
		t.Errorf("exit %d, stderr %q, calls %d; want a usage error and no call", code, errOut, svc.calls)
	}
}

// TestTaskGetHuman checks the human output of task get: the header, the goal, the criteria,
// the steps, the events with their folded text, and the cursor of the next page. It also
// checks that the request carries the section and the paging flags.
func TestTaskGetHuman(t *testing.T) {
	task := activeTask()
	svc := &fakeTasks{detail: &core.TaskDetail{
		Task:     task,
		Criteria: []core.Criterion{{Ord: 1, Criterion: "429 after 100 requests", Result: core.CriterionMet}},
		Steps:    twoSteps(),
		Events: []core.Event{{
			ID: 9, Kind: core.EventNote, Text: "started\x1b[31m the step",
			OccurredAt: time.Date(2026, 10, 30, 9, 2, 0, 0, time.UTC),
		}},
		Next: "c2",
	}}
	code, out, errOut := runTask(t, svc, "get", "task-20261030-01", "--section", "events", "--limit", "5", "--cursor", "c1")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	for _, want := range []string{
		"task-20261030-01  active  normal  Add rate limit\n",
		"Requests over 100/min get 429.\n",
		"1. 429 after 100 requests — met\n",
		"1. [active] Write test\n",
		"2026-10-30 09:02 UTC  note  started the step\n",
		"more events: pass --cursor c2 to read the next page\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if svc.gotGet.ID != "task-20261030-01" || svc.gotGet.Section != core.SectionEvents || svc.gotGet.Limit != 5 || svc.gotGet.Cursor != "c1" {
		t.Errorf("the request was %+v, want the id, section events, limit 5 and cursor c1", svc.gotGet)
	}
}

// TestTaskGetJSON checks the --json output of task get: one line, decoding to the task
// detail with the same id.
func TestTaskGetJSON(t *testing.T) {
	task := activeTask()
	svc := &fakeTasks{detail: &core.TaskDetail{Task: task}}
	code, out, errOut := runTask(t, svc, "get", "--json", "task-20261030-01")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	if !oneLine(out) {
		t.Fatalf("--json output is not one line: %q", out)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("--json output does not decode: %v: %s", err, out)
	}
	var tk core.Task
	if err := json.Unmarshal(got["task"], &tk); err != nil || tk.ID != task.ID {
		t.Errorf("the task in the --json output is %+v (%v), want %s", tk, err, task.ID)
	}
}

// TestTaskGetNotFound checks the error path of task get: the server's not_found text on
// stderr, nothing on stdout, exit 1.
func TestTaskGetNotFound(t *testing.T) {
	svc := &fakeTasks{err: core.NotFound("no task task-20261030-09 in this project")}
	code, out, errOut := runTask(t, svc, "get", "task-20261030-09")
	if code != 1 || out != "" || errOut != "cairn task get: no task task-20261030-09 in this project\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 1, nothing on stdout and the not-found line", code, out, errOut)
	}
}

// TestTaskGetNeedsOneID checks the usage error: get names exactly one task.
func TestTaskGetNeedsOneID(t *testing.T) {
	svc := &fakeTasks{}
	code, _, errOut := runTask(t, svc, "get")
	if code != 2 || !strings.Contains(errOut, "name one task id") || svc.calls != 0 {
		t.Errorf("exit %d, stderr %q, calls %d; want a usage error and no call", code, errOut, svc.calls)
	}
}

// TestTaskListHuman checks the human output of task list: one line per task, with each
// title folded so an escape sequence in it is gone, and the cursor of the next page.
func TestTaskListHuman(t *testing.T) {
	task := activeTask()
	svc := &fakeTasks{list: &core.TaskList{
		Tasks: []core.TaskSummary{{ID: task.ID, Status: core.TaskActive, Priority: core.PriorityNormal, Title: "Add\x1b]0;x\x07 rate limit"}},
		Next:  "c2",
	}}
	code, out, errOut := runTask(t, svc, "list")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	want := "task-20261030-01  active  normal  Add rate limit\nmore tasks: pass --cursor c2 to read the next page\n"
	if out != want {
		t.Errorf("stdout %q, want %q", out, want)
	}
}

// TestTaskListJSON checks the --json output of task list: one line, decoding to the list
// with its tasks and next cursor.
func TestTaskListJSON(t *testing.T) {
	svc := &fakeTasks{list: &core.TaskList{Tasks: []core.TaskSummary{{ID: "task-20261030-01", Title: "Add"}}, Next: "c2"}}
	code, out, errOut := runTask(t, svc, "list", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	if !oneLine(out) {
		t.Fatalf("--json output is not one line: %q", out)
	}
	var got core.TaskList
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("--json output does not decode: %v: %s", err, out)
	}
	if len(got.Tasks) != 1 || got.Next != "c2" {
		t.Errorf("--json output decodes to %+v, want one task and next c2", got)
	}
}

// TestTaskListPassesStatusesRepeated checks that --status may be given more than once, and
// that the limit and cursor reach the request.
func TestTaskListPassesStatusesRepeated(t *testing.T) {
	svc := &fakeTasks{list: &core.TaskList{}}
	code, _, errOut := runTask(t, svc, "list", "--status", "active", "--status", "blocked", "--limit", "3", "--cursor", "c1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	got := svc.gotList
	if len(got.Status) != 2 || got.Status[0] != core.TaskActive || got.Status[1] != core.TaskBlocked || got.Limit != 3 || got.Cursor != "c1" {
		t.Errorf("the request was %+v, want statuses active and blocked, limit 3, cursor c1", got)
	}
}

// TestTaskListError checks the error path of task list: the server's invalid text on
// stderr, nothing on stdout, exit 1.
func TestTaskListError(t *testing.T) {
	svc := &fakeTasks{err: core.Invalid("limit must be between 1 and %d", core.MaxLimit)}
	code, out, errOut := runTask(t, svc, "list", "--limit", "500")
	if code != 1 || out != "" || !strings.HasPrefix(errOut, "cairn task list: limit must be between 1 and") {
		t.Errorf("exit %d, stdout %q, stderr %q; want 1, nothing on stdout and the limit line", code, out, errOut)
	}
}

// TestTaskVerbs checks the usage errors of the command itself: no verb, and a verb it does
// not have. Each prints to stderr and exits 2, and makes no call.
func TestTaskVerbs(t *testing.T) {
	svc := &fakeTasks{}
	if code, _, errOut := runTask(t, svc); code != 2 || !strings.Contains(errOut, "cairn task current|get|list") {
		t.Errorf("no verb: exit %d, stderr %q", code, errOut)
	}
	if code, _, errOut := runTask(t, svc, "frobnicate"); code != 2 || !strings.Contains(errOut, `unknown verb "frobnicate"`) {
		t.Errorf("an unknown verb: exit %d, stderr %q", code, errOut)
	}
	if svc.calls != 0 {
		t.Errorf("a usage error made %d calls", svc.calls)
	}
}

// TestTaskOpenFailureStopsBeforeTheCall checks that when the project or the server cannot
// be resolved, the command says so on stderr and makes no call.
func TestTaskOpenFailureStopsBeforeTheCall(t *testing.T) {
	svc := &fakeTasks{}
	open := func() (Session, error) { return Session{}, core.Invalid("no project: run cairn init") }
	var out, errOut bytes.Buffer
	code := Task(open, []string{"list"}, &out, &errOut)
	if code != 1 || errOut.String() != "cairn task list: no project: run cairn init\n" || svc.calls != 0 {
		t.Errorf("exit %d, stderr %q, calls %d; want 1, the open's text and no call", code, errOut.String(), svc.calls)
	}
}

// TestTaskFlagErrors checks a flag the verb does not have: a usage error that names the
// flags to ask for, exit 2.
func TestTaskFlagErrors(t *testing.T) {
	svc := &fakeTasks{}
	code, _, errOut := runTask(t, svc, "list", "--nope")
	if code != 2 || !strings.Contains(errOut, "run cairn task list -h for the flags") || svc.calls != 0 {
		t.Errorf("exit %d, stderr %q, calls %d; want a flag usage error and no call", code, errOut, svc.calls)
	}
}

// TestTaskHelpPrintsItsFlags checks that -h prints the flags of the verb to stdout and exits 0.
func TestTaskHelpPrintsItsFlags(t *testing.T) {
	svc := &fakeTasks{}
	code, out, _ := runTask(t, svc, "list", "-h")
	if code != 0 || !strings.Contains(out, "-json") || !strings.Contains(out, "-status") {
		t.Errorf("exit %d, stdout %q; want 0 and the flags on stdout", code, out)
	}
}
