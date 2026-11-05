package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/djm56/cairn-memory/internal/core"
)

// callerFor returns a context that carries an agent caller in project, for seeding.
func callerFor(project string) context.Context {
	return core.WithCaller(context.Background(), core.Caller{Project: project, Agent: "main", Role: core.RoleAgent})
}

// TestExportPrintsTheTask checks the Markdown of an active task: its header, its goal,
// its steps with their status, and its journal.
func TestExportPrintsTheTask(t *testing.T) {
	local, url := newServer(t, nil)
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	id := seedTask(t, local, "acme-api")

	code, stdout, stderr := runCmd(t, "export", "task", id)
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stderr %q)", code, stderr)
	}
	for _, want := range []string{
		"# Wire the export\n",
		"- **ID:** " + id + "\n",
		"- **Status:** active\n",
		"## Goal\n\nPrint a task as Markdown.\n",
		"1. [active] Read the schema\n",
		"2. [pending] Write the printer\n",
		"## Journal\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("export lacks %q:\n%s", want, stdout)
		}
	}
	assertNoDestructive(t, stdout)
}

// TestExportWithNoIdPrintsTheActiveTask checks the default: the active task.
func TestExportWithNoIdPrintsTheActiveTask(t *testing.T) {
	local, url := newServer(t, nil)
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	id := seedTask(t, local, "acme-api")

	code, stdout, _ := runCmd(t, "export", "task")
	if code != 0 || !strings.Contains(stdout, "- **ID:** "+id+"\n") {
		t.Errorf("exit %d, stdout %q: want the active task %s", code, stdout, id)
	}
}

// TestExportWithNoActiveTaskNamesTheFix checks that asking for the active task when
// there is none exits 1 and names the id form, and does not guess.
func TestExportWithNoActiveTaskNamesTheFix(t *testing.T) {
	_, url := newServer(t, nil)
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")

	code, stdout, stderr := runCmd(t, "export", "task")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "give the task id") {
		t.Errorf("exit %d, stdout %q, stderr %q: want 1, no stdout and the id form named", code, stdout, stderr)
	}
}

// TestExportReportsAnUnknownTask checks that a task the project does not have is the
// server's not-found, with its message.
func TestExportReportsAnUnknownTask(t *testing.T) {
	_, url := newServer(t, nil)
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")

	code, stdout, stderr := runCmd(t, "export", "task", "task-20261030-99")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "not found") {
		t.Errorf("exit %d, stdout %q, stderr %q: want 1, no stdout and the not-found answer", code, stdout, stderr)
	}
}

// TestExportReadsEveryJournalPage checks the paging: a journal longer than one page
// (core.MaxLimit) is printed whole, and every event is in the output once.
func TestExportReadsEveryJournalPage(t *testing.T) {
	local, url := newServer(t, nil)
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	id := seedTask(t, local, "acme-api")
	const notes = core.MaxLimit + 25
	ctx := callerFor("acme-api")
	for i := 0; i < notes; i++ {
		if _, err := local.EventLog(ctx, core.EventLogRequest{TaskID: id, Kind: core.EventNote, Text: "note"}); err != nil {
			t.Fatalf("seed note %d: %v", i, err)
		}
	}

	code, stdout, stderr := runCmd(t, "export", "task", id)
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stderr %q)", code, stderr)
	}
	if got := strings.Count(stdout, "**note**"); got != notes {
		t.Errorf("export has %d notes, want %d", got, notes)
	}
}

// TestExportWritesDataAndFoldsLines checks that event data is printed on its own line,
// as compact JSON, and that a text with line breaks stays one list item.
func TestExportWritesDataAndFoldsLines(t *testing.T) {
	local, url := newServer(t, nil)
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	id := seedTask(t, local, "acme-api")
	_, err := local.EventLog(callerFor("acme-api"), core.EventLogRequest{
		TaskID: id, Kind: core.EventNote, Text: "line one\nline two",
		Data: json.RawMessage("{\n  \"k\": \"v\"\n}"),
	})
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}

	_, stdout, _ := runCmd(t, "export", "task", id)
	if !strings.Contains(stdout, "line one line two\n") {
		t.Errorf("the text is not folded to one item:\n%s", stdout)
	}
	if !strings.Contains(stdout, "  - data: {\"k\":\"v\"}\n") {
		t.Errorf("the data is not one compact line:\n%s", stdout)
	}
}

// TestExportUsageErrors checks the usage errors, each exit 2: no export named, an unknown export, and more
// than one id.
func TestExportUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"export"},
		{"export", "widget"},
		{"export", "task", "a", "b"},
	} {
		code, _, stderr := runCmd(t, args...)
		if code != 2 || stderr == "" {
			t.Errorf("%v: exit %d, stderr %q: want 2 and a message", args, code, stderr)
		}
	}
}

// seedClosedTask starts a task in project, finishes its one active step, and closes it as done, so the
// project can start another task. It returns the id.
func seedClosedTask(t *testing.T, local *core.Local, project, title string) string {
	t.Helper()
	id := seedNamedTask(t, local, project, title, "Read the schema first")
	finishStep(t, local, project, id, 1, "Schema read.")
	_, err := local.TaskClose(callerFor(project), core.TaskCloseRequest{
		ID: id, Outcome: core.TaskDone, Summary: "Done.",
		Criteria: []core.CriterionOutcome{}, Lessons: []core.Lesson{}, Mistakes: []core.Mistake{},
		NoLessonsReason: "Nothing new.",
	})
	if err != nil {
		t.Fatalf("close task %s: %v", id, err)
	}
	return id
}

// TestExportProjectPrintsEveryTask checks the project export over the server. Two closed tasks and an active
// one are written, in the order the task list gives them, and each is byte for byte what export task prints
// for it, with a blank line between two tasks. A task of another project is not written.
func TestExportProjectPrintsEveryTask(t *testing.T) {
	local, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	seedClosedTask(t, local, "acme-api", "First task")
	seedClosedTask(t, local, "acme-api", "Second task")
	active := seedNamedTask(t, local, "acme-api", "Third task", "Read the schema first")
	other := seedTask(t, local, "other-api")

	list, err := local.TaskList(callerFor("acme-api"), core.TaskListRequest{Limit: core.MaxLimit})
	if err != nil {
		t.Fatalf("list the tasks: %v", err)
	}
	var want []string
	for _, s := range list.Tasks {
		code, text, stderr := runCmd(t, "export", "task", s.ID)
		if code != 0 {
			t.Fatalf("export task %s: exit %d, stderr %q", s.ID, code, stderr)
		}
		want = append(want, text)
	}

	code, stdout, stderr := runCmd(t, "export", "project")
	if code != 0 || stderr != "" {
		t.Fatalf("export project: exit %d, stderr %q; want 0 and nothing on stderr", code, stderr)
	}
	if stdout != strings.Join(want, "\n") {
		t.Errorf("export project is not the task exports in list order:\n%s\nwant:\n%s", stdout, strings.Join(want, "\n"))
	}
	if len(list.Tasks) != 3 || !strings.Contains(stdout, "- **ID:** "+active+"\n") {
		t.Errorf("the project has %d tasks, want 3 with %s among them:\n%s", len(list.Tasks), active, stdout)
	}
	if strings.Contains(stdout, "- **ID:** "+other+"\n") {
		t.Errorf("export project writes a task of another project:\n%s", stdout)
	}
	assertNoDestructive(t, stdout)
}

// TestExportProjectOfAnEmptyProjectSaysSo checks that a project with no task prints the one line that says
// so, and exits 0.
func TestExportProjectOfAnEmptyProjectSaysSo(t *testing.T) {
	_, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")

	code, stdout, stderr := runCmd(t, "export", "project")
	if code != 0 || stderr != "" || stdout != "no tasks in acme-api\n" {
		t.Errorf("exit %d, stdout %q, stderr %q: want 0, the one line and nothing on stderr", code, stdout, stderr)
	}
}

// TestExportProjectReadsEveryTaskPage checks the paging of the task list: more tasks than one page
// (core.MaxLimit) are all written, each once.
func TestExportProjectReadsEveryTaskPage(t *testing.T) {
	local, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	const tasks = core.MaxLimit + 1
	for i := 0; i < tasks; i++ {
		seedClosedTask(t, local, "acme-api", fmt.Sprintf("Task %03d", i))
	}

	code, stdout, stderr := runCmd(t, "export", "project")
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stderr %q)", code, stderr)
	}
	if got := strings.Count(stdout, "- **ID:** "); got != tasks {
		t.Errorf("export project has %d tasks, want %d", got, tasks)
	}
}

// TestExportProjectTakesNoArgument checks the usage error: export project takes no argument, and a name
// after it is refused before anything is read.
func TestExportProjectTakesNoArgument(t *testing.T) {
	code, stdout, stderr := runCmd(t, "export", "project", "acme-api")
	if code != 2 || stdout != "" || stderr == "" {
		t.Errorf("exit %d, stdout %q, stderr %q: want 2, no stdout and a message", code, stdout, stderr)
	}
}

// TestAllPagesFollowsTheCursor checks the paging helper the exports share: each cursor is passed to the next
// call, and the items come back in page order.
func TestAllPagesFollowsTheCursor(t *testing.T) {
	pages := map[string]struct {
		items []int
		next  string
	}{
		"":  {[]int{1, 2}, "a"},
		"a": {[]int{3}, "b"},
		"b": {nil, ""},
	}
	got, err := allPages(func(cursor string) ([]int, string, error) {
		p, ok := pages[cursor]
		if !ok {
			return nil, "", fmt.Errorf("unexpected cursor %q", cursor)
		}
		return p.items, p.next, nil
	})
	if err != nil || !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("allPages = %v, %v; want 1 2 3 and no error", got, err)
	}
}

// TestAllPagesRefusesACursorThatComesBack checks that a cursor the server returns a second time ends the read
// with an error, so a server that loops cannot hang the command.
func TestAllPagesRefusesACursorThatComesBack(t *testing.T) {
	calls := 0
	_, err := allPages(func(cursor string) ([]int, string, error) {
		calls++
		if calls > 10 {
			t.Fatalf("the read did not stop after %d calls", calls)
		}
		return []int{calls}, "same", nil
	})
	if err == nil || !strings.Contains(err.Error(), `the cursor "same" came back`) {
		t.Errorf("allPages error = %v; want the cursor that came back named", err)
	}
}

// TestExitCheckMCPFlowThenExport is the M1 exit check (docs/PLAN.md §3), run against
// the in-process server: a small task is driven through cairn mcp over stdio, through
// internal/client, internal/api and core, closed, and then cairn export task prints
// it. The server is the same one for both commands, as it would be on a machine.
func TestExitCheckMCPFlowThenExport(t *testing.T) {
	_, url := newServer(t, nil)
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CLAUDE_PROJECT_DIR", t.TempDir())

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	var stderr bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- serveMCP(nil, inR, outW, &stderr) }()

	ctx := context.Background()
	mc := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "exit-check", Version: "0"}, nil)
	cs, err := mc.Connect(ctx, &mcpsdk.IOTransport{Reader: outR, Writer: inW}, nil)
	if err != nil {
		t.Fatalf("connect over stdio: %v", err)
	}
	do := func(name string, args map[string]any) string {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: name, Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%s over stdio: %v, %+v", name, err, res)
		}
		return res.Content[0].(*mcpsdk.TextContent).Text
	}

	var started struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(do("task_start", map[string]any{
		"agent": "main", "title": "Exit check", "goal": "Drive one task through cairn mcp.",
		"criteria": []string{"The export prints it"},
		"steps":    []map[string]any{{"title": "Start"}, {"title": "Finish"}},
	})), &started); err != nil || started.ID == "" {
		t.Fatalf("task_start result: %v", err)
	}
	do("step_update", map[string]any{"agent": "main", "task_id": started.ID, "step": map[string]any{"number": 1}, "action": "start"})
	do("step_update", map[string]any{"agent": "main", "task_id": started.ID, "step": map[string]any{"number": 1}, "action": "finish", "summary": "Started."})
	do("step_update", map[string]any{"agent": "main", "task_id": started.ID, "step": map[string]any{"number": 2}, "action": "start"})
	do("step_update", map[string]any{"agent": "main", "task_id": started.ID, "step": map[string]any{"number": 2}, "action": "finish", "summary": "Finished."})
	do("task_close", map[string]any{
		"agent": "main", "id": started.ID, "outcome": "done", "summary": "The export prints the task.",
		"criteria": []map[string]any{{"ord": 1, "result": "met", "evidence": "TestExitCheckMCPFlowThenExport"}},
		"lessons":  []map[string]any{{"kind": "reuse", "text": "Drive the flow over stdio.", "why": "It is the real protocol."}},
		"mistakes": []any{},
	})

	if err := cs.Close(); err != nil {
		t.Fatalf("close the client session: %v", err)
	}
	_ = inW.Close()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serveMCP exit code %d, want 0 (stderr %q)", code, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serveMCP did not return after the client closed")
	}

	code, stdout, stderrOut := runCmd(t, "export", "task", started.ID)
	if code != 0 {
		t.Fatalf("export task: exit %d (stderr %q)", code, stderrOut)
	}
	for _, want := range []string{"# Exit check", "- **Status:** done", "- [x] The export prints it — met", "## Lessons", "1. [done] Start", "2. [done] Finish"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("export lacks %q:\n%s", want, stdout)
		}
	}
}

// TestExportFoldsServerText checks, through export, that a title with a carriage
// return and a Markdown heading stays one heading line. A goal keeps its line break, as
// a paragraph does, and loses its escape sequence. A key file's path and its note are one
// line each, with the separator and the control character gone.
func TestExportFoldsServerText(t *testing.T) {
	local, url := newServer(t, nil)
	dir := t.TempDir()
	useDir(t, dir)
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	res, err := local.TaskStart(callerFor("acme-api"), core.TaskStartRequest{
		Title:    "Ship\r\n# Forged\x1b[2J heading",
		Goal:     "Line one\r\nline two\x1b[31m red",
		KeyFiles: []core.TaskFile{{Path: "src/a\u2028.go", Role: core.FileKey, Note: "note\x1b[1m\rhere"}},
		Steps:    []core.StepInput{{Title: "Write it"}},
	})
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}

	code, stdout, stderr := runCmd(t, "export", "task", res.ID)
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stderr %q)", code, stderr)
	}
	for _, want := range []string{
		"# Ship # Forged heading\n",
		"## Goal\n\nLine one\nline two red\n",
		"- `src/a .go` (key): note here\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("export lacks %q:\n%s", want, stdout)
		}
	}
	headings := 0
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasPrefix(line, "# ") {
			headings++
		}
	}
	if headings != 1 {
		t.Errorf("export has %d single-# headings, want the title only:\n%s", headings, stdout)
	}
	if strings.ContainsAny(stdout, "\r\x1b\x07\u2028") {
		t.Errorf("export carries a control character or a separator: %q", stdout)
	}
}

// TestExportCodeSpanHoldsABacktick checks that a backtick in a key file's path
// does not end the code span the path sits in: the fence is longer than any run of
// backticks in the path, and a path that begins or ends with one is padded.
func TestExportCodeSpanHoldsABacktick(t *testing.T) {
	cases := map[string]string{
		"src/plain.go": "- `src/plain.go`\n",
		"a`b.go":       "- ``a`b.go``\n",
		"a``b.go":      "- ```a``b.go```\n",
		"`tick.go":     "- `` `tick.go ``\n",
		"tick.go`":     "- `` tick.go` ``\n",
	}
	for path, want := range cases {
		if got := fileLine(core.TaskFile{Path: path}) + "\n"; got != want {
			t.Errorf("fileLine(%q) = %q, want %q", path, got, want)
		}
	}
}
