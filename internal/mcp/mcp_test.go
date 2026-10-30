package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/core"
	cairnmcp "github.com/djm56/cairn-memory/internal/mcp"
	"github.com/djm56/cairn-memory/internal/store"
)

// clientName is the name the test MCP client gives itself at initialisation.
const clientName = "test-client"

// wantTools are the fifteen tools of DESIGN.md §4, by name. The operator calls
// are not among them, and must not be.
var wantTools = []string{
	"task_current", "task_start", "task_get", "task_list", "task_update", "task_close",
	"step_plan", "step_add", "step_update", "event_log",
	"learning_add", "learning_list", "directive_list", "recall", "search",
}

// newService serves a real core.Local over the api handlers on an httptest server
// bound to loopback (D19), and returns the client for it. The Local is returned
// too, so a test can read a row back through the core directly.
func newService(t *testing.T) (core.Service, *core.Local) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cairn.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	local := core.NewLocal(st)
	srv := httptest.NewServer(api.Handler(local, api.UnauthenticatedHeaderCaller, nil))
	t.Cleanup(srv.Close)
	if !strings.HasPrefix(srv.URL, "http://127.0.0.1:") {
		t.Fatalf("test server at %s is not on loopback", srv.URL)
	}
	c, err := client.New(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	return c, local
}

// connect runs an MCP server for svc in project over an in-memory transport, and
// returns the test client's session with it. The protocol is the real one: the
// SDK frames every message, exactly as it does on stdio.
func connect(t *testing.T, svc core.Service, project cairnmcp.Project) *mcpsdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	serverT, clientT := mcpsdk.NewInMemoryTransports()
	srv := cairnmcp.New(svc, project, "test")
	go func() { _ = srv.Run(ctx, serverT) }()

	mc := mcpsdk.NewClient(&mcpsdk.Implementation{Name: clientName, Version: "0"}, nil)
	cs, err := mc.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("connect the MCP client: %v", err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// acme is the project of every test: a valid name, resolved with no error.
var acme = cairnmcp.Project{Name: "acme-api"}

// call invokes tool name with args (a JSON object, or a value that encodes to
// one) and returns the text of its one result block, and whether it is an error.
func call(t *testing.T, cs *mcpsdk.ClientSession, name string, args any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("%s: %d content blocks, want 1", name, len(res.Content))
	}
	block, ok := res.Content[0].(*mcpsdk.TextContent)
	if !ok {
		t.Fatalf("%s: content is %T, want text", name, res.Content[0])
	}
	return block.Text, res.IsError
}

// mustCall is call for a result that must succeed. It returns the decoded result.
func mustCall(t *testing.T, cs *mcpsdk.ClientSession, name string, args any) map[string]any {
	t.Helper()
	text, isErr := call(t, cs, name, args)
	if isErr {
		t.Fatalf("%s returned a tool error: %s", name, text)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%s: result %q is not a JSON object: %v", name, text, err)
	}
	return out
}

// errorOf decodes the error body of a tool error result, and fails the test when
// the result is not one.
func errorOf(t *testing.T, text string, isErr bool) struct {
	Kind    string   `json:"kind"`
	Message string   `json:"message"`
	Missing []string `json:"missing"`
} {
	t.Helper()
	if !isErr {
		t.Fatalf("result %q is not a tool error", text)
	}
	var body struct {
		Error struct {
			Kind    string   `json:"kind"`
			Message string   `json:"message"`
			Missing []string `json:"missing"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &body); err != nil || body.Error.Kind == "" {
		t.Fatalf("tool error %q is not the error body: %v", text, err)
	}
	return body.Error
}

// TestEveryToolIsListedWithItsSchema checks that the server offers the fifteen
// tools of DESIGN.md §4 and no operator call, and that each tool has a schema and
// a description under 200 characters (PLAN.md §6 and CONTRIBUTING rule 8).
func TestEveryToolIsListedWithItsSchema(t *testing.T) {
	svc, _ := newService(t)
	cs := connect(t, svc, acme)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.InputSchema == nil {
			t.Errorf("%s has no input schema", tool.Name)
		}
		if n := len([]rune(tool.Description)); n == 0 || n >= 200 {
			t.Errorf("%s: description is %d characters, want 1 to 199", tool.Name, n)
		}
	}
	want := slices.Clone(wantTools)
	slices.Sort(want)
	slices.Sort(names)
	if !slices.Equal(names, want) {
		t.Errorf("tools = %v\nwant    %v", names, want)
	}
}

// TestTaskFlowReachesEveryTool runs a small task through the server: start with a
// plan, work a step, log a review, insert and replan steps, record a learning,
// refuse a close, then close. Every one of the fifteen tools is called, and each
// result is checked against the state the flow should have made.
func TestTaskFlowReachesEveryTool(t *testing.T) {
	svc, _ := newService(t)
	cs := connect(t, svc, acme)

	start := mustCall(t, cs, "task_start", map[string]any{
		"agent": "main", "title": "Add rate limit", "goal": "Requests over 100 a minute get 429.",
		"criteria": []string{"429 after 100 requests"},
		"steps":    []map[string]any{{"title": "Write test"}, {"title": "Implement"}},
	})
	taskID, _ := start["id"].(string)
	if taskID == "" || start["status"] != "active" {
		t.Fatalf("task_start = %v, want an active task with an id", start)
	}
	if steps, _ := start["steps"].([]any); len(steps) != 2 {
		t.Fatalf("task_start steps = %v, want two", start["steps"])
	}

	current := mustCall(t, cs, "task_current", nil)
	if task, _ := current["task"].(map[string]any); task["id"] != taskID {
		t.Fatalf("task_current = %v, want task %s", current, taskID)
	}
	if dirs, ok := current["directives"]; ok && dirs != nil {
		// No directive is ratified in this test, so the list is empty or absent.
		t.Errorf("task_current directives = %v, want none", dirs)
	}

	mustCall(t, cs, "step_update", map[string]any{
		"agent": "main", "task_id": taskID, "step": map[string]any{"number": 1}, "action": "start",
	})
	mustCall(t, cs, "event_log", map[string]any{
		"agent": "main", "task_id": taskID, "kind": "note", "text": "Test first.",
	})
	mustCall(t, cs, "step_update", map[string]any{
		"agent": "main", "task_id": taskID, "step": map[string]any{"number": 1}, "action": "finish",
		"summary": "The test fails as it should.",
	})
	mustCall(t, cs, "event_log", map[string]any{
		"agent": "reviewer", "task_id": taskID, "step": map[string]any{"number": 1}, "kind": "review",
		"text": "Test read.",
		"data": json.RawMessage(`{"verdict": "approved", "critical": 0, "warnings": 1}`),
	})

	// The journal, read as a section and paged (task_get, section events).
	events := mustCall(t, cs, "task_get", map[string]any{"id": taskID, "section": "events", "limit": 2})
	if list, _ := events["events"].([]any); len(list) != 2 {
		t.Errorf("task_get events page has %d entries, want 2", len(list))
	}
	if events["next"] == nil || events["next"] == "" {
		t.Errorf("task_get events: no next cursor on a page of 2 with more to come")
	}

	// The brief reads back what the start wrote (step 8 review S4: results are asserted).
	brief := mustCall(t, cs, "task_get", map[string]any{"id": taskID, "section": "brief"})
	if task, _ := brief["task"].(map[string]any); task["goal"] != "Requests over 100 a minute get 429." {
		t.Errorf("task_get brief = %v, want the goal the start wrote", brief)
	}
	if criteria, _ := brief["criteria"].([]any); len(criteria) != 1 {
		t.Errorf("task_get brief criteria = %v, want the one criterion", brief["criteria"])
	}
	mustCall(t, cs, "task_get", map[string]any{"id": taskID})
	active := mustCall(t, cs, "task_list", map[string]any{"status": []string{"active"}})
	if tasks, _ := active["tasks"].([]any); len(tasks) != 1 || tasks[0].(map[string]any)["id"] != taskID {
		t.Errorf("task_list active = %v, want only %s", active["tasks"], taskID)
	}

	// Replan: a pending step is kept only when it has no event. Step 2 is pending
	// and has none, so the new plan replaces it, and step 1 (finished) is kept.
	planned := mustCall(t, cs, "step_plan", map[string]any{
		"agent": "main", "task_id": taskID,
		"steps": []map[string]any{{"title": "Implement the limit"}},
	})
	if steps, _ := planned["steps"].([]any); len(steps) != 2 {
		t.Errorf("step_plan = %v, want the finished step and the new one", planned)
	}

	// Insert after step 1, and reassign the new step to hicks: the caller stays main.
	added := mustCall(t, cs, "step_add", map[string]any{
		"agent": "main", "task_id": taskID, "after": map[string]any{"number": 1},
		"title": "Wire the middleware", "assignee": "hicks",
	})
	if added["agent"] != "hicks" {
		t.Errorf("step_add agent = %v, want the assignee hicks", added["agent"])
	}
	updated := mustCall(t, cs, "task_update", map[string]any{
		"agent": "main", "id": taskID, "next_action": "wire the middleware", "owner": "main",
	})
	if updated["next_action"] != "wire the middleware" || updated["owner"] != "main" {
		t.Errorf("task_update = %v, want the next action and owner it set", updated)
	}

	// A learning is proposed, and the list shows it as proposed.
	mustCall(t, cs, "learning_add", map[string]any{
		"agent": "main", "task_id": taskID, "kind": "finding", "title": "Limit in one place",
		"body": "The limit could live in the router.",
	})
	learnings := mustCall(t, cs, "learning_list", map[string]any{"status": []string{"proposed"}})
	if list, _ := learnings["learnings"].([]any); len(list) != 1 {
		t.Errorf("learning_list = %v, want the one proposed learning", learnings)
	}

	// No directive is ratified here, so the lists are present and empty.
	for _, args := range []map[string]any{{}, {"agent": "reviewer"}} {
		dirs := mustCall(t, cs, "directive_list", args)
		if list, _ := dirs["directives"].([]any); len(list) != 0 {
			t.Errorf("directive_list %v = %v, want none", args, list)
		}
	}
	recalled := mustCall(t, cs, "recall", map[string]any{"goal": "rate limit the API"})
	if tasks, _ := recalled["tasks"].([]any); !slices.ContainsFunc(tasks, func(v any) bool {
		return v.(map[string]any)["id"] == taskID
	}) {
		t.Errorf("recall tasks = %v, want %s", recalled["tasks"], taskID)
	}
	// The task is found by its title and goal. "middleware" finds no hit here, although
	// the next action and a step title hold it, so the query is one the title answers.
	searched := mustCall(t, cs, "search", map[string]any{"query": "rate limit"})
	if hits, _ := searched["hits"].([]any); !slices.ContainsFunc(hits, func(v any) bool {
		h := v.(map[string]any)
		return h["id"] == taskID && h["kind"] == "task"
	}) {
		t.Errorf("search hits = %v, want a task hit on %s", searched["hits"], taskID)
	}

	// Close is refused, and the refusal lists what is missing: no summary, no
	// criteria results, no lessons, no mistakes.
	text, isErr := call(t, cs, "task_close", map[string]any{"agent": "main", "id": taskID, "outcome": "done"})
	refused := errorOf(t, text, isErr)
	if refused.Kind != "refused" {
		t.Fatalf("task_close kind = %q, want refused", refused.Kind)
	}
	for _, key := range []string{"summary", "criteria", "lessons", "mistakes"} {
		if !slices.Contains(refused.Missing, key) {
			t.Errorf("task_close missing = %v, want %q listed", refused.Missing, key)
		}
	}

	// The close that meets the policy: the step being done, criteria met, a lesson
	// and an empty mistakes list.
	closed := mustCall(t, cs, "task_close", map[string]any{
		"agent": "main", "id": taskID, "outcome": "done", "summary": "The limit is in.",
		"criteria": []map[string]any{{"ord": 1, "result": "met", "evidence": "the test passes"}},
		"lessons":  []map[string]any{{"kind": "reuse", "text": "Test first.", "why": "It caught the edge."}},
		"mistakes": []any{},
	})
	if closed["status"] != "done" {
		t.Errorf("task_close status = %v, want done", closed["status"])
	}
}

// TestToolErrorsCarryKindMessageAndMissing checks the error mapping of a tool:
// a core.Error is a tool error with its kind, message and missing list, in the
// body the HTTP form uses, and the result is IsError and not a protocol error.
func TestToolErrorsCarryKindMessageAndMissing(t *testing.T) {
	svc, _ := newService(t)
	cs := connect(t, svc, acme)

	text, isErr := call(t, cs, "task_get", map[string]any{"id": "task-20261030-99"})
	if got := errorOf(t, text, isErr); got.Kind != "not_found" {
		t.Errorf("task_get of a missing task: kind %q, want not_found (%s)", got.Kind, text)
	}

	text, isErr = call(t, cs, "task_start", map[string]any{"agent": "main", "title": "t"})
	if got := errorOf(t, text, isErr); got.Kind != "invalid" || !strings.Contains(got.Message, "goal") {
		t.Errorf("task_start with no goal: %+v, want invalid and a message that names goal", got)
	}

	text, isErr = call(t, cs, "event_log", map[string]any{"task_id": "task-20261030-01", "kind": "note", "text": "x"})
	if got := errorOf(t, text, isErr); got.Kind != "invalid" {
		t.Errorf("event_log with no agent: kind %q, want invalid (%s)", got.Kind, text)
	}

	text, isErr = call(t, cs, "event_log", map[string]any{"agent": "main", "task_id": "task-20261030-01", "kind": "started", "text": "x"})
	if got := errorOf(t, text, isErr); got.Kind != "invalid" {
		t.Errorf("event_log of kind started: kind %q, want invalid (%s)", got.Kind, text)
	}
}

// TestProjectErrorRefusesEveryCall checks DESIGN §2.1: when the project did not
// resolve, the server still starts, and each call is refused with that error, so
// the model sees the fix (cairn init) and not a silent default project.
func TestProjectErrorRefusesEveryCall(t *testing.T) {
	svc, _ := newService(t)
	noProject := cairnmcp.Project{Err: core.ErrNoProject}
	cs := connect(t, svc, noProject)

	for _, name := range []string{"task_current", "task_list", "search"} {
		text, isErr := call(t, cs, name, map[string]any{})
		got := errorOf(t, text, isErr)
		if got.Kind != "invalid" || !strings.Contains(got.Message, "cairn init") {
			t.Errorf("%s with no project: %+v, want invalid and a message that names cairn init", name, got)
		}
	}
}

// TestArgumentValidation checks what the adapter itself refuses: a field a tool
// does not name, a value of the wrong type, and a value a tool needs but does not
// have. Each is a tool error of kind invalid, and the message names the problem.
func TestArgumentValidation(t *testing.T) {
	svc, _ := newService(t)
	cs := connect(t, svc, acme)
	cases := []struct {
		what string
		tool string
		args string // a JSON object
		want string // a word the message must contain
	}{
		{"an unknown field", "task_list", `{"limt": 5}`, "limt"},
		{"an unknown field on a write", "task_start", `{"agent":"main","title":"t","goal":"g","priorty":"high"}`, "priorty"},
		{"a string for a number", "task_list", `{"limit": "ten"}`, "limit"},
		{"a list for a string", "task_get", `{"id": ["task-1"]}`, "id"},
		{"a missing id", "task_get", `{}`, "task id"},
		{"a step ref with two keys", "step_update", `{"agent":"main","task_id":"task-1","step":{"id":1,"number":1},"action":"start"}`, "step"},
	}
	for _, c := range cases {
		text, isErr := call(t, cs, c.tool, json.RawMessage(c.args))
		got := errorOf(t, text, isErr)
		if got.Kind != "invalid" {
			t.Errorf("%s (%s): kind %q, want invalid (%s)", c.what, c.tool, got.Kind, text)
			continue
		}
		if !strings.Contains(got.Message, c.want) {
			t.Errorf("%s (%s): message %q does not contain %q", c.what, c.tool, got.Message, c.want)
		}
	}
}

// TestRepeatedReviewKeyReachesCore checks CONTRACT §4 through the protocol: a
// review whose data repeats a key is refused. The SDK's typed handlers would keep
// the last of the two keys and accept it, so this fails if the arguments are
// decoded through a map.
func TestRepeatedReviewKeyReachesCore(t *testing.T) {
	svc, _ := newService(t)
	cs := connect(t, svc, acme)
	start := mustCall(t, cs, "task_start", map[string]any{
		"agent": "main", "title": "t", "goal": "g", "steps": []map[string]any{{"title": "one"}},
	})
	taskID := start["id"].(string)

	args := json.RawMessage(`{"agent":"reviewer","task_id":"` + taskID + `","kind":"review","text":"x",` +
		`"data":{"verdict":"not-approved","verdict":"approved","critical":1,"warnings":0}}`)
	text, isErr := call(t, cs, "event_log", args)
	got := errorOf(t, text, isErr)
	if got.Kind != "invalid" || !strings.Contains(got.Message, `repeats the key "verdict"`) {
		t.Errorf("review with a repeated verdict: %+v, want invalid naming the repeated key (%s)", got, text)
	}
}

// TestCallerCarriesProjectAgentAndClient checks identity (CONTRACT §1.1). A write
// carries the agent argument as the caller's agent, a read carries none, every call
// is in the project, the role is agent, and the client is the one that initialised
// the MCP session.
func TestCallerCarriesProjectAgentAndClient(t *testing.T) {
	inner, _ := newService(t)
	rec := &recorder{Service: inner}
	cs := connect(t, rec, acme)

	mustCall(t, cs, "task_start", map[string]any{"agent": "hicks", "title": "t", "goal": "g"})
	if got := rec.last; got.Project != "acme-api" || got.Agent != "hicks" || got.Client != clientName || got.Role != core.RoleAgent {
		t.Errorf("write caller = %+v, want project acme-api, agent hicks, client %s, role agent", got, clientName)
	}

	mustCall(t, cs, "task_current", nil)
	if got := rec.last; got.Project != "acme-api" || got.Agent != "" || got.Role != core.RoleAgent {
		t.Errorf("read caller = %+v, want project acme-api, no agent, role agent", got)
	}
}

// recorder is a Service that keeps the caller of the last TaskStart and TaskCurrent.
type recorder struct {
	core.Service
	last core.Caller
}

func (r *recorder) TaskStart(ctx context.Context, req core.TaskStartRequest) (*core.TaskStartResult, error) {
	r.last, _ = core.CallerFrom(ctx)
	return r.Service.TaskStart(ctx, req)
}

func (r *recorder) TaskCurrent(ctx context.Context) (*core.TaskCurrent, error) {
	r.last, _ = core.CallerFrom(ctx)
	return r.Service.TaskCurrent(ctx)
}

// TestUnreachableServerIsAnInternalError checks that a failure which is not a
// core.Error, here a server that is not there, is a tool error of kind internal,
// with the cause in its text, and not a protocol error that ends the session.
func TestUnreachableServerIsAnInternalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	c, err := client.New(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs := connect(t, c, acme)
	text, isErr := call(t, cs, "task_current", nil)
	got := errorOf(t, text, isErr)
	if got.Kind != "internal" || got.Message == "" {
		t.Errorf("unreachable server: %+v, want kind internal with its cause (%s)", got, text)
	}
}

// TestSchemaNamesEventData checks that the schema of event_log describes data as an
// object, since a model reads the schema, and not as the byte array the inference
// would otherwise give.
func TestSchemaNamesEventData(t *testing.T) {
	svc, _ := newService(t)
	cs := connect(t, svc, acme)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "event_log" {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		// Other properties are nullable pointers, whose type is a list. Only data is read.
		var schema struct {
			Properties map[string]struct {
				Type any `json:"type"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		if got := schema.Properties["data"].Type; got != "object" {
			t.Errorf("event_log data schema type = %v, want object (schema %s)", got, raw)
		}
		return
	}
	t.Error("event_log is not listed")
}
