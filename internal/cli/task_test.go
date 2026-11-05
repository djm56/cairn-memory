package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/client"
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
	code := Task(open, args, strings.NewReader(""), &out, &errOut)
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
	code := Task(open, []string{"list"}, strings.NewReader(""), &out, &errOut)
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

// TestTaskCurrentPrintsEveryPart checks that the summary of task current prints each part the
// call read: the goal and context, the criteria with evidence, the steps with their assignee,
// the recent events, each directive with its code, scope, title and rule, and the counts. A
// next action of 250 runes is printed whole, since the summary cuts nothing.
func TestTaskCurrentPrintsEveryPart(t *testing.T) {
	task := activeTask()
	task.Context = "Two limits, one per key."
	task.NextAction = strings.Repeat("n", 250)
	steps := twoSteps()
	steps[1].Agent = "hicks"
	at := time.Date(2026, 10, 30, 9, 2, 0, 0, time.UTC)
	svc := &fakeTasks{cur: &core.TaskCurrent{
		Task:     &task,
		Criteria: []core.Criterion{{Ord: 1, Criterion: "429 after 100 requests", Result: core.CriterionMet, Evidence: "test ok"}},
		Steps:    steps,
		RecentEvents: []core.Event{{
			ID: 9, Kind: core.EventNote, Text: "started", OccurredAt: at,
		}},
		Directives: []core.Directive{
			{Code: "D-0004", Scope: core.ScopeGlobal, Title: "Test first", Rule: "Write the failing test before the code."},
			{Code: "D-0009", Scope: core.ScopeAgent, Agent: "reviewer", Title: "Read the diff", Rule: "Read every changed\nline."},
		},
		Expired: 2, Omitted: 3, OtherActive: 1,
	}}
	code, out, errOut := runTask(t, svc, "current")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	for _, want := range []string{
		"next        " + strings.Repeat("n", 250) + "\n",
		"context     Two limits, one per key.\n",
		"other       1 other active task(s)\n",
		"criteria\n  1. 429 after 100 requests — met: test ok\n",
		"steps\n  1. [active] Write test\n  2. [pending] Implement (agent hicks)\n",
		"recent events, newest first\n  " + stamp(at) + "  note  started\n",
		"directives  2 apply to this project\n",
		"  D-0004  global  Test first\n          Write the failing test before the code.\n",
		"  D-0009  agent reviewer  Read the diff\n          Read every changed line.\n",
		"expired     2 past review_by, left out\n",
		"omitted     3 beyond the cap of 12\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// TestTaskGetPrintsKeyFilesAndDeliverables checks that the summary of task get prints the key
// files of the brief and the deliverables of the close, each with its note when it has one.
func TestTaskGetPrintsKeyFilesAndDeliverables(t *testing.T) {
	task := activeTask()
	task.Status = core.TaskDone
	svc := &fakeTasks{detail: &core.TaskDetail{
		Task: task,
		KeyFiles: []core.TaskFile{
			{Path: "internal/limit/limit.go", Role: core.FileKey, Note: "the limiter"},
			{Path: "internal/limit/limit_test.go", Role: core.FileKey},
		},
		Deliverables: []core.TaskFile{{Path: "docs/limit.md", Role: core.FileDeliverable, Note: "the write-up"}},
	}}
	code, out, errOut := runTask(t, svc, "get", "task-20261030-01")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	for _, want := range []string{
		"key files\n  internal/limit/limit.go — the limiter\n  internal/limit/limit_test.go\n",
		"deliverables\n  docs/limit.md — the write-up\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

// TestTaskJSONEscapesTerminalCharacters checks that --json writes the characters a terminal can
// act on as escapes: U+009B (the single-byte CSI), U+202E (a bidi override), DEL and U+0085. The
// characters are built from their code points, so this file holds only ASCII. The line is still
// one line of JSON, and it decodes to the title the server sent.
func TestTaskJSONEscapesTerminalCharacters(t *testing.T) {
	csi := string(rune(0x9b))
	bidi := string(rune(0x202e))
	nel := string(rune(0x85))
	del := string(rune(0x7f))
	title := "Add" + csi + "2J rate" + bidi + " limit" + del + nel
	task := activeTask()
	task.Title = title
	svc := &fakeTasks{detail: &core.TaskDetail{Task: task}}
	code, out, errOut := runTask(t, svc, "get", "--json", "task-20261030-01")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	if !oneLine(out) {
		t.Fatalf("--json output is not one line: %q", out)
	}
	for _, raw := range []string{csi, bidi, del, nel} {
		if strings.Contains(out, raw) {
			t.Errorf("--json output holds the raw character %U", []rune(raw)[0])
		}
	}
	for _, r := range []rune{0x9b, 0x202e, 0x7f, 0x85} {
		esc := `\u` + hex4(r)
		if !strings.Contains(out, esc) {
			t.Errorf("--json output lacks the escape %s", esc)
		}
	}
	var got core.TaskDetail
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("--json output does not decode: %v: %s", err, out)
	}
	if got.Task.Title != title {
		t.Errorf("the title decodes to %q, want %q", got.Task.Title, title)
	}
}

// TestTaskJSONEscapesEveryFormatCharacter checks that --json escapes each Unicode format character
// (category Cf), not only the bidi overrides and isolates: the left-to-right and arabic letter
// marks, the zero-width characters, the word joiner, the soft hyphen, the byte order mark, and a
// tag character above U+FFFF, which is written as a surrogate pair. Each is built from its code
// point, and the title decodes back to the same text.
func TestTaskJSONEscapesEveryFormatCharacter(t *testing.T) {
	for _, r := range []rune{0x200e, 0x200f, 0x061c, 0x200b, 0xfeff, 0x00ad, 0x2060, 0x2069, 0xe0001, 0xe0041} {
		ch := string(r)
		task := activeTask()
		task.Title = "a" + ch + "b"
		svc := &fakeTasks{detail: &core.TaskDetail{Task: task}}
		code, out, errOut := runTask(t, svc, "get", "--json", "task-20261030-01")
		if code != 0 || errOut != "" || !oneLine(out) {
			t.Fatalf("%U: exit %d, stderr %q, output %q; want one line and exit 0", r, code, errOut, out)
		}
		if strings.Contains(out, ch) {
			t.Errorf("--json output holds the raw character %U", r)
		}
		if esc := jsonEscapeOf(r); !strings.Contains(out, esc) {
			t.Errorf("--json output lacks the escape %s for %U", esc, r)
		}
		var got core.TaskDetail
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("%U: --json output does not decode: %v", r, err)
		}
		if got.Task.Title != task.Title {
			t.Errorf("%U: the title decodes to %+q, want %+q", r, got.Task.Title, task.Title)
		}
	}
}

// jsonEscapeOf returns the escape --json writes for r: one \u escape up to U+FFFF, and the
// surrogate pair above it.
func jsonEscapeOf(r rune) string {
	if r <= 0xffff {
		return `\u` + hex4(r)
	}
	r -= 0x10000
	return `\u` + hex4(0xd800+(r>>10)) + `\u` + hex4(0xdc00+(r&0x3ff))
}

// hex4 returns the four lower-case hex digits of r, as a JSON escape holds them.
func hex4(r rune) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[r>>12&0xf], digits[r>>8&0xf], digits[r>>4&0xf], digits[r&0xf]})
}

// TestTaskDoubleDashMakesEverythingPositional checks that after "--" a flag is a positional: the
// --json after the terminator is read as the task id, not as the flag, so the output is the
// human form.
func TestTaskDoubleDashMakesEverythingPositional(t *testing.T) {
	svc := &fakeTasks{detail: &core.TaskDetail{Task: activeTask()}}
	code, out, errOut := runTask(t, svc, "get", "--", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	if svc.gotGet.ID != "--json" {
		t.Errorf("the id read was %q, want --json", svc.gotGet.ID)
	}
	if strings.HasPrefix(out, "{") {
		t.Errorf("the --json after -- was read as the flag: %q", out)
	}
}

// TestTaskDoubleDashCountsPositionals checks that the arguments after "--" count as positionals,
// so two of them are a usage error whatever they look like, and nothing is called.
func TestTaskDoubleDashCountsPositionals(t *testing.T) {
	svc := &fakeTasks{}
	if code, _, errOut := runTask(t, svc, "get", "--", "-x", "--json"); code != 2 || !strings.Contains(errOut, "name one task id") || svc.calls != 0 {
		t.Errorf("get with two ids: exit %d, stderr %q, calls %d; want a usage error and no call", code, errOut, svc.calls)
	}
	if code, _, errOut := runTask(t, svc, "list", "--", "-x"); code != 2 || !strings.Contains(errOut, `takes no task id, got ["-x"]`) || svc.calls != 0 {
		t.Errorf("list with an id: exit %d, stderr %q, calls %d; want a usage error and no call", code, errOut, svc.calls)
	}
}

// TestTaskFlagsBeforeDoubleDash checks that a flag before "--" is still read, and that a flag's
// value may begin with a dash, as the flag package reads it.
func TestTaskFlagsBeforeDoubleDash(t *testing.T) {
	svc := &fakeTasks{detail: &core.TaskDetail{Task: activeTask()}}
	code, out, errOut := runTask(t, svc, "get", "--json", "--", "task-20261030-01")
	if code != 0 || errOut != "" || !oneLine(out) || svc.gotGet.ID != "task-20261030-01" {
		t.Errorf("exit %d, stderr %q, id %q, output %q; want the json of the named task", code, errOut, svc.gotGet.ID, out)
	}
	list := &fakeTasks{list: &core.TaskList{}}
	if code, _, errOut := runTask(t, list, "list", "--cursor", "-c1"); code != 0 || list.gotList.Cursor != "-c1" {
		t.Errorf("exit %d, stderr %q, cursor %q; want the cursor -c1", code, errOut, list.gotList.Cursor)
	}
}

// TestTaskLimitBelowOneIsAUsageError checks that an explicit --limit below 1 is a usage error in
// each verb that pages, with no call: the default is to leave the flag out, so 0 is not it.
func TestTaskLimitBelowOneIsAUsageError(t *testing.T) {
	svc := &fakeTasks{list: &core.TaskList{}, detail: &core.TaskDetail{Task: activeTask()}}
	for _, args := range [][]string{
		{"list", "--limit", "0"},
		{"list", "--limit", "-3"},
		{"get", "task-20261030-01", "--limit", "0"},
	} {
		code, _, errOut := runTask(t, svc, args...)
		if code != 2 || !strings.Contains(errOut, "--limit must be 1 or more") {
			t.Errorf("%v: exit %d, stderr %q; want a usage error", args, code, errOut)
		}
	}
	if svc.calls != 0 {
		t.Errorf("the usage errors made %d calls", svc.calls)
	}
}

// TestTaskStatusNeedsAName checks that an empty --status is a usage error and makes no call, so
// an empty status is never sent to the server. Leaving the flag out is how a person lists all.
func TestTaskStatusNeedsAName(t *testing.T) {
	svc := &fakeTasks{list: &core.TaskList{}}
	code, _, errOut := runTask(t, svc, "list", "--status", "")
	if code != 2 || !strings.Contains(errOut, "a status must be named") || svc.calls != 0 {
		t.Errorf("exit %d, stderr %q, calls %d; want a usage error and no call", code, errOut, svc.calls)
	}
}

// TestTaskListHelpSaysHowToListAll checks that the help of --status says to leave it out to list
// every status, and that the help of --limit says 0 is not the default.
func TestTaskListHelpSaysHowToListAll(t *testing.T) {
	svc := &fakeTasks{}
	_, out, _ := runTask(t, svc, "list", "-h")
	for _, want := range []string{"leave it out to list all", "leave it out for the default"} {
		if !strings.Contains(out, want) {
			t.Errorf("the help lacks %q:\n%s", want, out)
		}
	}
}

// TestTaskGetStepFilter checks that --step n sends the step as a number ref, the form the MCP
// tool takes, and that no ref is sent without the flag. A step below 1 is a usage error, with no
// call, since number 0 names no step outside step_add.
func TestTaskGetStepFilter(t *testing.T) {
	svc := &fakeTasks{detail: &core.TaskDetail{Task: activeTask()}}
	if code, _, errOut := runTask(t, svc, "get", "task-20261030-01", "--section", "events", "--step", "2"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if ref := svc.gotGet.Step; ref == nil || ref.Number == nil || *ref.Number != 2 || ref.ID != nil {
		t.Errorf("the step ref was %+v, want number 2", ref)
	}
	plain := &fakeTasks{detail: &core.TaskDetail{Task: activeTask()}}
	if code, _, errOut := runTask(t, plain, "get", "task-20261030-01"); code != 0 || plain.gotGet.Step != nil {
		t.Errorf("exit %d, stderr %q, step ref %+v; want no ref without --step", code, errOut, plain.gotGet.Step)
	}
	refused := &fakeTasks{}
	if code, _, errOut := runTask(t, refused, "get", "task-20261030-01", "--step", "0"); code != 2 || !strings.Contains(errOut, "--step must be a step number") || refused.calls != 0 {
		t.Errorf("--step 0: exit %d, stderr %q, calls %d; want a usage error and no call", code, errOut, refused.calls)
	}
}

// TestTaskServerErrorIsCappedToOneLine checks the error path of a message the server wrote: it
// is one line of at most 200 runes, ending in an ellipsis, with no escape sequence in it.
func TestTaskServerErrorIsCappedToOneLine(t *testing.T) {
	msg := "first line\nsecond \x1b[31mline " + strings.Repeat("x", 300)
	svc := &fakeTasks{err: &client.ServerError{Err: core.Invalid("%s", msg)}}
	code, out, errOut := runTask(t, svc, "list")
	if code != 1 || out != "" || !oneLine(errOut) {
		t.Fatalf("exit %d, stdout %q, stderr %q; want 1, nothing on stdout and one line on stderr", code, out, errOut)
	}
	reason := strings.TrimPrefix(errOut, "cairn task list: ")
	if n := len([]rune(strings.TrimSuffix(reason, "\n"))); n != MaxLineRunes {
		t.Errorf("the reason is %d runes, want %d", n, MaxLineRunes)
	}
	if !strings.HasSuffix(errOut, "…\n") || strings.Contains(errOut, "\x1b") {
		t.Errorf("stderr %q should end in an ellipsis and hold no escape sequence", errOut)
	}
}

// TestTaskLocalErrorIsNotCut checks that an error the client or a local resolver built is printed
// whole, as cli.Reason gives it, since it names a path or a fix and a cut would hide it.
func TestTaskLocalErrorIsNotCut(t *testing.T) {
	long := strings.Repeat("y", 300)
	svc := &fakeTasks{err: core.Invalid("%s", long)}
	code, _, errOut := runTask(t, svc, "list")
	if code != 1 || errOut != "cairn task list: "+long+"\n" {
		t.Errorf("exit %d, stderr of %d bytes; want the whole message", code, len(errOut))
	}
}

// TestCurrentStepFallsBack checks which step the summary names: the active one, else the first
// pending one, else none. A step that is done or skipped is never named.
func TestCurrentStepFallsBack(t *testing.T) {
	steps := []core.Step{
		{Number: 1, Status: core.StepDone},
		{Number: 2, Status: core.StepPending},
		{Number: 3, Status: core.StepPending},
	}
	if s := currentStep(steps); s == nil || s.Number != 2 {
		t.Errorf("with no active step, the current step is %+v, want the first pending one, 2", s)
	}
	steps[2].Status = core.StepActive
	if s := currentStep(steps); s == nil || s.Number != 3 {
		t.Errorf("with an active step, the current step is %+v, want the active one, 3", s)
	}
	if s := currentStep([]core.Step{{Number: 1, Status: core.StepDone}, {Number: 2, Status: core.StepSkipped}}); s != nil {
		t.Errorf("with only done and skipped steps, the current step is %+v, want none", s)
	}
	if s := currentStep(nil); s != nil {
		t.Errorf("with no steps, the current step is %+v, want none", s)
	}
}

// TestTaskCurrentOmitsTheStepLineWhenNoStepIsOpen checks that the summary prints no step line
// when no step is active or pending, so it does not name a step that is finished.
func TestTaskCurrentOmitsTheStepLineWhenNoStepIsOpen(t *testing.T) {
	task := activeTask()
	svc := &fakeTasks{cur: &core.TaskCurrent{Task: &task, Steps: []core.Step{{Number: 1, Title: "Write test", Status: core.StepDone}}}}
	_, out, _ := runTask(t, svc, "current")
	if strings.Contains(out, "step        ") {
		t.Errorf("the output names a step though none is open:\n%s", out)
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
