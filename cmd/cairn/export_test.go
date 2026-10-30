package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

// TestExportUsageErrors checks the usage errors, each exit 2: no export named, a
// project export that is not built, an unknown export, and more than one id.
func TestExportUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"export"},
		{"export", "project"},
		{"export", "widget"},
		{"export", "task", "a", "b"},
	} {
		code, _, stderr := runCmd(t, args...)
		if code != 2 || stderr == "" {
			t.Errorf("%v: exit %d, stderr %q: want 2 and a message", args, code, stderr)
		}
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
