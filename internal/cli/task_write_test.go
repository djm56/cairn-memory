package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// startedResult is the answer of task_start for a task with the two steps of twoSteps.
func startedResult() *core.TaskStartResult {
	return &core.TaskStartResult{ID: "task-20261030-01", Status: core.TaskActive, Steps: twoSteps()}
}

// TestTaskStartHuman checks the human output of task start: the id and status, then the plan,
// and that the request carries the brief and the agent the flag named.
func TestTaskStartHuman(t *testing.T) {
	t.Setenv(EnvAgent, "")
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
	t.Setenv(EnvAgent, "")
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
	t.Setenv(EnvAgent, "")
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
	t.Setenv(EnvAgent, "")
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
	t.Setenv(EnvAgent, "")
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
	t.Setenv(EnvAgent, "")
	svc := &fakeWrites{start: startedResult()}
	code, out, errOut := runWrite(t, Task, svc, "", "start", "--title", "t", "--goal", "g")
	if code != 2 || out != "" || svc.calls != 0 {
		t.Errorf("exit %d, stdout %q, calls %d; want a usage error and no call", code, out, svc.calls)
	}
	for _, want := range []string{"cairn task start: no agent name", "--agent", EnvAgent} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr %q lacks %q", errOut, want)
		}
	}
}

// TestTaskStartAgentFromTheVariable checks that CAIRN_AGENT names the agent when --agent is left out.
func TestTaskStartAgentFromTheVariable(t *testing.T) {
	t.Setenv(EnvAgent, "hicks")
	svc := &fakeWrites{start: startedResult()}
	if code, _, errOut := runWrite(t, Task, svc, "", "start", "--title", "t", "--goal", "g"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := agentOf(t, svc.ctx); got != "hicks" {
		t.Errorf("the call ran as %q, want hicks from %s", got, EnvAgent)
	}
}

// TestTaskStartRefusedByTheServer checks a refusal of the server: its words on stderr, one line,
// nothing on stdout, exit 1.
func TestTaskStartRefusedByTheServer(t *testing.T) {
	t.Setenv(EnvAgent, "")
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
	t.Setenv(EnvAgent, "")
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

// TestTaskStartHelpNamesTheVariable checks that -h names the agent variable and the stdin forms,
// so the fallback and the "-" are written where a person reads the flags.
func TestTaskStartHelpNamesTheVariable(t *testing.T) {
	svc := &fakeWrites{}
	_, out, _ := runWrite(t, Task, svc, "", "start", "-h")
	for _, want := range []string{EnvAgent, "-json-input", "reads standard input"} {
		if !strings.Contains(out, want) {
			t.Errorf("the help lacks %q:\n%s", want, out)
		}
	}
}
