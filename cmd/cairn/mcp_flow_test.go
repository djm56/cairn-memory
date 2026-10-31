package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"runtime/pprof"
	"slices"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// stdioConnectTimeout bounds the client's connect over the pipes. A connect that does not
// finish is a fault to report, with the cause, not a hang of the whole package (step 12
// review 12-W4).
const stdioConnectTimeout = 10 * time.Second

// goroutineDump returns the stacks of every goroutine, for a connect that timed out.
func goroutineDump() string {
	var b bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&b, 2)
	return b.String()
}

// stdioSession runs cairn mcp over pipes, as a client starts it, and returns the
// client's session with it. The connect selects on the process exit as well as its own
// result, so a serveMCP that returns early is reported with its exit code and stderr, not
// as a connect that hung. On timeout it reports stderr and a goroutine dump. The returned
// finish closes the session, and checks that cairn mcp returned 0 once the client went away
// and wrote nothing to stderr.
func stdioSession(t *testing.T) (*mcpsdk.ClientSession, func()) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	stderr := &syncWriter{}
	done := make(chan int, 1)
	go func() { done <- serveMCP(nil, inR, outW, stderr) }()

	mc := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "stdio-flow", Version: "0"}, nil)
	type connected struct {
		cs  *mcpsdk.ClientSession
		err error
	}
	ready := make(chan connected, 1)
	go func() {
		cs, err := mc.Connect(context.Background(), &mcpsdk.IOTransport{Reader: outR, Writer: inW}, nil)
		ready <- connected{cs: cs, err: err}
	}()
	var cs *mcpsdk.ClientSession
	select {
	case c := <-ready:
		if c.err != nil {
			t.Fatalf("connect over stdio: %v (stderr %q)", c.err, stderr.String())
		}
		cs = c.cs
	case code := <-done:
		t.Fatalf("cairn mcp returned %d before the client connected (stderr %q)", code, stderr.String())
	case <-time.After(stdioConnectTimeout):
		t.Fatalf("connect over stdio timed out after %s (stderr %q)\ngoroutines:\n%s", stdioConnectTimeout, stderr.String(), goroutineDump())
	}
	finish := func() {
		if err := cs.Close(); err != nil {
			t.Errorf("close the client session: %v", err)
		}
		_ = inW.Close()
		select {
		case code := <-done:
			if code != 0 {
				t.Errorf("serveMCP exit code %d, want 0 (stderr %q)", code, stderr.String())
			}
		case <-time.After(10 * time.Second):
			t.Error("serveMCP did not return after the client closed")
		}
		if out := stderr.String(); out != "" {
			t.Errorf("serveMCP wrote to stderr: %q", out)
		}
	}
	return cs, finish
}

// stdioDo calls tool name over cs with args, and returns its result as a JSON object.
// A protocol error, or a tool error, fails the test with the text of the error.
func stdioDo(t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	text, isErr := stdioText(t, cs, name, args)
	if isErr {
		t.Fatalf("%s over stdio returned a tool error: %s", name, text)
	}
	return decodeObject(t, name, text)
}

// stdioText calls tool name over cs and returns the text of its one result, and whether
// the result is a tool error.
func stdioText(t *testing.T, cs *mcpsdk.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s over stdio: protocol error: %v", name, err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s over stdio: %d content blocks, want 1", name, len(res.Content))
	}
	block, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("%s over stdio: content is %T, want text", name, res.Content[0])
	}
	return block.Text, res.IsError
}

// decodeObject reads a result as a JSON object, failing the test when it is not one.
func decodeObject(t *testing.T, name, text string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil || out == nil {
		t.Fatalf("%s: result %q is not a JSON object: %v", name, text, err)
	}
	return out
}

// listOf returns the list under key in obj, failing the test when it is missing.
func listOf(t *testing.T, obj map[string]any, key string) []any {
	t.Helper()
	list, ok := obj[key].([]any)
	if !ok {
		t.Fatalf("result has no list %q: %v", key, obj)
	}
	return list
}

// fieldOf returns the object under key in obj, failing the test when it is missing.
func fieldOf(t *testing.T, obj map[string]any, key string) map[string]any {
	t.Helper()
	m, ok := obj[key].(map[string]any)
	if !ok {
		t.Fatalf("result has no object %q: %v", key, obj)
	}
	return m
}

// TestMCPStdioFlowAssertsEachResult is the MCP-level test of the task flow over stdio
// (#18; docs/PLAN.md §3, M1). It drives the whole flow through cairn mcp: a task started
// with a plan, its brief read back, a step started and finished, a replan, a step added to
// the end, a review and a note in the journal, a learning proposed and listed, the
// directives and the recall and search reads, a task list and an update, a refused close,
// and a close. tools/list names the fifteen tools of DESIGN §4, and no other. Every call's
// result is read and asserted, not only the fact that it succeeded (step 8 review S4; step
// 12 review W1).
//
// It runs twice: "env", with the server and project in the environment, and "toml", with
// them in .cairn.toml (12-W4: the toml form was never in the suite; the stdio hang of step
// 12 was seen on it).
func TestMCPStdioFlowAssertsEachResult(t *testing.T) {
	t.Run("env", func(t *testing.T) {
		_, url := newServer(t, nil)
		useDir(t, t.TempDir())
		t.Setenv("CAIRN_SERVER", url)
		t.Setenv("CAIRN_PROJECT", "acme-api")
		t.Setenv("CLAUDE_PROJECT_DIR", t.TempDir())
		runStdioFlow(t)
	})
	t.Run("toml", func(t *testing.T) {
		_, url := newServer(t, nil)
		dir := t.TempDir()
		useDir(t, dir)
		writeFile(t, dir, ".cairn.toml", "project = \"acme-api\"\nserver = \""+url+"\"\n")
		t.Setenv("CLAUDE_PROJECT_DIR", dir)
		runStdioFlow(t)
	})
}

// runStdioFlow drives the whole flow over stdio, asserting each result. The caller has set
// where the server and the project come from.
func runStdioFlow(t *testing.T) {
	cs, finish := stdioSession(t)
	defer finish()

	// tools/list: the fifteen names, sorted, and no other tool.
	listed, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list over stdio: %v", err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	wantNames := []string{
		"directive_list", "event_log", "learning_add", "learning_list", "recall", "search",
		"step_add", "step_plan", "step_update", "task_close", "task_current", "task_get",
		"task_list", "task_start", "task_update",
	}
	if !slices.Equal(names, wantNames) {
		t.Errorf("tools/list = %v, want %v", names, wantNames)
	}

	started := stdioDo(t, cs, "task_start", map[string]any{
		"agent": "main", "title": "Add rate limit", "goal": "Requests over 100 a minute get 429.",
		"criteria": []string{"429 after 100 requests"},
		"steps":    []map[string]any{{"title": "Write test"}, {"title": "Implement"}},
	})
	id, _ := started["id"].(string)
	if id == "" || started["status"] != "active" {
		t.Fatalf("task_start = %v, want an active task with an id", started)
	}
	if steps := listOf(t, started, "steps"); len(steps) != 2 || asObj(t, steps[0])["status"] != "pending" {
		t.Fatalf("task_start steps = %v, want two pending steps", steps)
	}

	current := stdioDo(t, cs, "task_current", nil)
	if task := fieldOf(t, current, "task"); task["id"] != id || task["title"] != "Add rate limit" {
		t.Errorf("task_current task = %v, want %s titled Add rate limit", task, id)
	}

	// The brief, read back: the goal and the criterion the start wrote.
	brief := stdioDo(t, cs, "task_get", map[string]any{"id": id, "section": "brief"})
	if task := fieldOf(t, brief, "task"); task["goal"] != "Requests over 100 a minute get 429." {
		t.Errorf("task_get brief goal = %v", task["goal"])
	}
	if criteria := listOf(t, brief, "criteria"); len(criteria) != 1 || asObj(t, criteria[0])["criterion"] != "429 after 100 requests" {
		t.Errorf("task_get brief criteria = %v, want the one criterion", criteria)
	}

	started1 := stdioDo(t, cs, "step_update", map[string]any{"agent": "main", "task_id": id, "step": map[string]any{"number": 1}, "action": "start"})
	if started1["number"] != float64(1) || started1["status"] != "active" {
		t.Errorf("step_update start of step 1 = %v, want step 1 active", started1)
	}
	note := stdioDo(t, cs, "event_log", map[string]any{"agent": "main", "task_id": id, "kind": "note", "text": "Test first."})
	if note["kind"] != "note" || note["text"] != "Test first." || note["agent"] != "main" {
		t.Errorf("event_log note = %v, want a note by main reading Test first.", note)
	}
	finished1 := stdioDo(t, cs, "step_update", map[string]any{"agent": "main", "task_id": id, "step": map[string]any{"number": 1}, "action": "finish", "summary": "The test fails as it should."})
	if finished1["status"] != "done" || finished1["summary"] != "The test fails as it should." {
		t.Errorf("step_update finish of step 1 = %v, want step 1 done with its summary", finished1)
	}

	// Replan: step 1 is finished and kept; step 2 is pending with no event, so it is replaced.
	planned := stdioDo(t, cs, "step_plan", map[string]any{
		"agent": "main", "task_id": id, "steps": []map[string]any{{"title": "Implement the limit"}},
	})
	steps := listOf(t, planned, "steps")
	if len(steps) != 2 || asObj(t, steps[0])["status"] != "done" || asObj(t, steps[1])["title"] != "Implement the limit" {
		t.Errorf("step_plan steps = %v, want the done step and the new one", steps)
	}

	// The review, logged by another agent on step 1, comes back as the event it made.
	review := stdioDo(t, cs, "event_log", map[string]any{
		"agent": "reviewer", "task_id": id, "step": map[string]any{"number": 1}, "kind": "review",
		"text": "Test read.", "data": json.RawMessage(`{"verdict": "approved", "critical": 0, "warnings": 1}`),
	})
	if review["kind"] != "review" || review["agent"] != "reviewer" || fieldOf(t, review, "data")["verdict"] != "approved" {
		t.Errorf("event_log review = %v, want a review by reviewer with verdict approved", review)
	}

	// The journal, newest first: the review is the first event, and a page of two has more.
	events := stdioDo(t, cs, "task_get", map[string]any{"id": id, "section": "events", "limit": 2})
	if list := listOf(t, events, "events"); len(list) != 2 || asObj(t, list[0])["kind"] != "review" {
		t.Errorf("task_get events = %v, want two events with the review first", list)
	}
	if events["next"] == nil || events["next"] == "" {
		t.Errorf("task_get events: no cursor for the rest of the journal")
	}

	started2 := stdioDo(t, cs, "step_update", map[string]any{"agent": "main", "task_id": id, "step": map[string]any{"number": 2}, "action": "start"})
	if started2["number"] != float64(2) || started2["status"] != "active" {
		t.Errorf("step_update start of step 2 = %v, want step 2 active", started2)
	}
	finished2 := stdioDo(t, cs, "step_update", map[string]any{"agent": "main", "task_id": id, "step": map[string]any{"number": 2}, "action": "finish", "summary": "Limited."})
	if finished2["status"] != "done" || finished2["summary"] != "Limited." {
		t.Errorf("step_update finish of step 2 = %v, want step 2 done with its summary", finished2)
	}

	// A step added with no position goes last, numbered 3, for hicks, and is pending.
	added := stdioDo(t, cs, "step_add", map[string]any{"agent": "main", "task_id": id, "title": "Document the limit", "assignee": "hicks"})
	if added["number"] != float64(3) || added["title"] != "Document the limit" || added["agent"] != "hicks" || added["status"] != "pending" {
		t.Errorf("step_add = %v, want step 3, Document the limit, for hicks, pending", added)
	}

	// A learning is proposed, and listed as proposed with its title.
	learned := stdioDo(t, cs, "learning_add", map[string]any{
		"agent": "main", "task_id": id, "kind": "finding", "title": "Limit in one place", "body": "The limit could live in the router.",
	})
	if learned["title"] != "Limit in one place" || learned["status"] != "proposed" {
		t.Errorf("learning_add = %v, want the proposed learning with its title", learned)
	}
	learnings := stdioDo(t, cs, "learning_list", map[string]any{"status": []string{"proposed"}})
	if list := listOf(t, learnings, "learnings"); len(list) != 1 || asObj(t, list[0])["title"] != "Limit in one place" || asObj(t, list[0])["status"] != "proposed" {
		t.Errorf("learning_list = %v, want the one proposed learning", learnings)
	}

	// No directive is ratified over MCP, so the lists are present and empty.
	for _, args := range []map[string]any{{}, {"agent": "reviewer"}} {
		dirs := stdioDo(t, cs, "directive_list", args)
		if list := listOf(t, dirs, "directives"); len(list) != 0 {
			t.Errorf("directive_list %v = %v, want none", args, list)
		}
	}

	// Recall and search find the task by its own words.
	recalled := stdioDo(t, cs, "recall", map[string]any{"goal": "rate limit the API"})
	if list := listOf(t, recalled, "tasks"); !taskIDIn(t, list, id) {
		t.Errorf("recall tasks = %v, want %s", list, id)
	}
	searched := stdioDo(t, cs, "search", map[string]any{"query": "rate limit"})
	if list := listOf(t, searched, "hits"); !hitIn(t, list, id) {
		t.Errorf("search hits = %v, want a hit on %s", list, id)
	}

	// The active list has the task, and nothing else.
	list := stdioDo(t, cs, "task_list", map[string]any{"status": []string{"active"}})
	if tasks := listOf(t, list, "tasks"); len(tasks) != 1 || asObj(t, tasks[0])["id"] != id {
		t.Errorf("task_list active = %v, want only %s", tasks, id)
	}

	// The update changes what it names, and returns the task with the change.
	updated := stdioDo(t, cs, "task_update", map[string]any{
		"agent": "main", "id": id, "next_action": "wire the middleware", "owner": "main",
	})
	if updated["next_action"] != "wire the middleware" || updated["owner"] != "main" {
		t.Errorf("task_update = %v, want the next action and owner it set", updated)
	}

	// A close without its fields is refused, and the refusal names what is missing.
	text, isErr := stdioText(t, cs, "task_close", map[string]any{"agent": "main", "id": id, "outcome": "done"})
	refused := errorOf(t, text, isErr)
	if refused.Kind != "refused" || !slices.Contains(refused.Missing, "summary") || !slices.Contains(refused.Missing, "lessons") {
		t.Errorf("task_close without fields: %+v, want refused, naming summary and lessons", refused)
	}

	closed := stdioDo(t, cs, "task_close", map[string]any{
		"agent": "main", "id": id, "outcome": "done", "summary": "The limit is in.",
		"criteria": []map[string]any{{"ord": 1, "result": "met", "evidence": "the test passes"}},
		"lessons":  []map[string]any{{"kind": "reuse", "text": "Test first.", "why": "It caught the edge."}},
		"mistakes": []any{},
	})
	if closed["status"] != "done" || closed["outcome_summary"] != "The limit is in." {
		t.Errorf("task_close = %v, want done with the summary", closed)
	}

	current = stdioDo(t, cs, "task_current", nil)
	if current["task"] != nil {
		t.Errorf("task_current after the close = %v, want no active task", current["task"])
	}
}

// asObj returns v as a JSON object, failing the test when it is not one.
func asObj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%v is not a JSON object", v)
	}
	return m
}

// taskIDIn reports whether a list of task summaries holds the task id.
func taskIDIn(t *testing.T, list []any, id string) bool {
	t.Helper()
	for _, v := range list {
		if asObj(t, v)["id"] == id {
			return true
		}
	}
	return false
}

// hitIn reports whether a list of search hits holds a hit on the task id, of kind task.
func hitIn(t *testing.T, list []any, id string) bool {
	t.Helper()
	for _, v := range list {
		h := asObj(t, v)
		if h["id"] == id && h["kind"] == "task" {
			return true
		}
	}
	return false
}

// errorBody is the error of a tool error result, in the form the HTTP error answer uses.
type errorBody struct {
	Kind    string   `json:"kind"`
	Message string   `json:"message"`
	Missing []string `json:"missing"`
}

// errorOf decodes the error of a tool error result, and fails the test when the result
// is not one.
func errorOf(t *testing.T, text string, isErr bool) errorBody {
	t.Helper()
	if !isErr {
		t.Fatalf("result %q is not a tool error", text)
	}
	var body struct {
		Error errorBody `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &body); err != nil || body.Error.Kind == "" {
		t.Fatalf("tool error %q is not the error body: %v", text, err)
	}
	return body.Error
}
