package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// primeRun runs Prime with svc behind it, as the project acme-api, and returns its exit code, stdout,
// stderr and the number of opens. The opener sets the project the way cmd/cairn does.
func primeRun(svc core.Service, args ...string) (int, string, string, int) {
	opened := 0
	open := func() (Session, error) {
		opened++
		return Session{Svc: svc, Ctx: core.WithCaller(context.Background(), testCaller), Project: "acme-api"}, nil
	}
	var out, errOut bytes.Buffer
	code := Prime(open, args, &out, &errOut)
	return code, out.String(), errOut.String(), opened
}

// primeBlockNoRole is the session-start block prime writes for activeTask with no role: the block the
// hook writes, closed by the CLI pointer rather than the MCP one.
const primeBlockNoRole = `Cairn task: "Add rate limit" (task-20261030-01, active) in acme-api.
Next action: write the test
Current step: 1 of 2, "Write test" (active).
Directives that apply: 0.
Agent-scoped directives were not counted: no agent role is set.
Run cairn task current for the brief, the steps and recent events; cairn directive list for all directives.
`

// TestPrimeHumanWithAnActiveTask checks the human output of prime with a task and no role: the block,
// then a blank line, then the summary task current writes for the same answer. It makes one call.
func TestPrimeHumanWithAnActiveTask(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	task := activeTask()
	svc := &fakeTasks{cur: &core.TaskCurrent{Task: &task, Steps: twoSteps()}}
	code, out, errOut, opened := primeRun(svc)
	if code != 0 || errOut != "" || opened != 1 || svc.calls != 1 {
		t.Fatalf("exit %d, stderr %q, opens %d, calls %d; want 0, nothing on stderr, one open and one call", code, errOut, opened, svc.calls)
	}
	block, summary, found := strings.Cut(out, "\n\n")
	if !found {
		t.Fatalf("no blank line between the block and the summary:\n%s", out)
	}
	if block+"\n" != primeBlockNoRole {
		t.Errorf("session-start block:\n%q\nwant:\n%q", block+"\n", primeBlockNoRole)
	}
	for _, want := range []string{
		"task        task-20261030-01  active  Add rate limit\n",
		"next        write the test\n",
		"step        1 of 2: Write test (active)\n",
		"directives  0 apply to this project\n",
	} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, summary)
		}
	}
}

// TestPrimeHumanWithNoTask checks that with no active task prime writes the block's one line and stops:
// no blank line and no summary follow it, and the exit is 0.
func TestPrimeHumanWithNoTask(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeTasks{cur: &core.TaskCurrent{}}
	code, out, errOut, _ := primeRun(svc)
	if code != 0 || errOut != "" || out != "Cairn: no task is active in acme-api.\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want 0 and the one no-task line", code, out, errOut)
	}
}

// TestPrimeWithARole checks that a role names the caller and leaves the no-role sentence out of the
// block. --agent wins, and CAIRN_AGENT names the role when the flag is left out.
func TestPrimeWithARole(t *testing.T) {
	task := activeTask()
	svc := &fakeTasks{cur: &core.TaskCurrent{Task: &task, Steps: twoSteps()}}

	t.Setenv(identity.EnvAgent, "")
	code, out, errOut, _ := primeRun(svc, "--agent", "reviewer")
	if code != 0 || errOut != "" || strings.Contains(out, "not counted") {
		t.Errorf("--agent: exit %d, stderr %q, output %q; want the block with no no-role sentence", code, errOut, out)
	}
	if c, _ := core.CallerFrom(svc.gotCtx); c.Agent != "reviewer" || c.Project != "acme-api" {
		t.Errorf("the call ran as %+v, want role reviewer in acme-api", c)
	}

	t.Setenv(identity.EnvAgent, "hicks")
	code, out, errOut, _ = primeRun(svc)
	if code != 0 || errOut != "" || strings.Contains(out, "not counted") {
		t.Errorf("CAIRN_AGENT: exit %d, stderr %q; want the block with no no-role sentence", code, errOut)
	}
	if c, _ := core.CallerFrom(svc.gotCtx); c.Agent != "hicks" {
		t.Errorf("with %s set the call ran as %q, want hicks", identity.EnvAgent, c.Agent)
	}
}

// TestPrimeJSON checks the --json output of prime field by field: exactly four keys, the project, the
// role, role_refused as false, and the current result, which decodes to the task and its steps.
func TestPrimeJSON(t *testing.T) {
	task := activeTask()
	svc := &fakeTasks{cur: &core.TaskCurrent{Task: &task, Steps: twoSteps()}}
	code, out, errOut, _ := primeRun(svc, "--json", "--agent", "reviewer")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q; want 0, nothing on stderr and one line of JSON, got %q", code, errOut, out)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &keys); err != nil {
		t.Fatalf("the output is not a JSON object: %v\n%s", err, out)
	}
	if len(keys) != 4 {
		t.Errorf("the object has %d keys, want exactly project, role, role_refused and current", len(keys))
	}
	var got struct {
		Project     string           `json:"project"`
		Role        string           `json:"role"`
		RoleRefused bool             `json:"role_refused"`
		Current     core.TaskCurrent `json:"current"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("the output does not decode: %v\n%s", err, out)
	}
	if got.Project != "acme-api" {
		t.Errorf("project = %q, want acme-api", got.Project)
	}
	if got.Role != "reviewer" {
		t.Errorf("role = %q, want reviewer", got.Role)
	}
	if got.RoleRefused {
		t.Errorf("role_refused = true, want false")
	}
	if got.Current.Task == nil || got.Current.Task.ID != task.ID || len(got.Current.Steps) != 2 {
		t.Errorf("current decodes to %+v, want task %s with two steps", got.Current, task.ID)
	}
}

// TestPrimeJSONCurrentIsTaskCurrents checks that the current object is the task current result exactly as
// task current --json writes it, byte for byte, both with a task and with none.
func TestPrimeJSONCurrentIsTaskCurrents(t *testing.T) {
	task := activeTask()
	for name, cur := range map[string]*core.TaskCurrent{
		"with a task":  {Task: &task, Steps: twoSteps()},
		"with no task": {},
	} {
		t.Run(name, func(t *testing.T) {
			svc := &fakeTasks{cur: cur}
			code, out, errOut, _ := primeRun(svc, "--json")
			if code != 0 || errOut != "" {
				t.Fatalf("prime: exit %d, stderr %q", code, errOut)
			}
			_, taskOut, _ := runTask(t, svc, "current", "--json")
			var keys map[string]json.RawMessage
			if err := json.Unmarshal([]byte(out), &keys); err != nil {
				t.Fatalf("the output is not a JSON object: %v", err)
			}
			if got, want := string(keys["current"]), strings.TrimSuffix(taskOut, "\n"); got != want {
				t.Errorf("current = %s\nwant task current --json = %s", got, want)
			}
		})
	}
}

// TestPrimeJSONWithNoTask checks that the JSON for no active task still has the four keys, with the
// task null, and no role when none was given.
func TestPrimeJSONWithNoTask(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeTasks{cur: &core.TaskCurrent{}}
	code, out, errOut, _ := primeRun(svc, "--json")
	if code != 0 || errOut != "" || !oneLine(out) {
		t.Fatalf("exit %d, stderr %q, output %q; want one line of JSON", code, errOut, out)
	}
	if got := jsonField(t, out, "role"); got != `""` {
		t.Errorf("role = %s, want the empty string", got)
	}
	if got := jsonField(t, out, "project"); got != `"acme-api"` {
		t.Errorf("project = %s, want acme-api", got)
	}
	var cur struct {
		Task *core.Task `json:"task"`
	}
	if err := json.Unmarshal([]byte(jsonField(t, out, "current")), &cur); err != nil || cur.Task != nil {
		t.Errorf("current = %s, want a task_current result with task null (err %v)", jsonField(t, out, "current"), err)
	}
}

// TestPrimeRefusedAgentIsAUsageError checks that a name the wire form refuses is a usage error, exit 2,
// from --agent or from CAIRN_AGENT, and that a positional argument is one too. Each opens nothing and
// makes no call.
func TestPrimeRefusedAgentIsAUsageError(t *testing.T) {
	cases := []struct {
		name string
		env  string
		args []string
		want string
	}{
		{"a refused --agent", "", []string{"--agent", "hicks "}, "the name from --agent is refused"},
		{"a refused CAIRN_AGENT", "hicks ", nil, "the name from " + identity.EnvAgent + " is refused"},
		{"a positional argument", "", []string{"extra"}, "takes no positional argument"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(identity.EnvAgent, c.env)
			svc := &fakeTasks{cur: &core.TaskCurrent{}}
			code, out, errOut, opened := primeRun(svc, c.args...)
			if code != 2 || out != "" || !strings.Contains(errOut, c.want) || opened != 0 || svc.calls != 0 {
				t.Errorf("exit %d, stdout %q, stderr %q, opens %d, calls %d; want a usage error naming %q with no open or call",
					code, out, errOut, opened, svc.calls, c.want)
			}
		})
	}
}

// TestPrimeServerRefusal checks that a refusal of the server exits 1, with the reason on stderr and
// nothing on stdout.
func TestPrimeServerRefusal(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	svc := &fakeTasks{err: core.Refused("the task could not be read")}
	code, out, errOut, _ := primeRun(svc)
	if code != 1 || out != "" || errOut != "cairn prime: the task could not be read\n" {
		t.Errorf("exit %d, stdout %q, stderr %q; want exit 1 with the refusal on stderr", code, out, errOut)
	}
}

// TestPrimeHelp checks that -h prints the flags and exits 0 before any project is opened.
func TestPrimeHelp(t *testing.T) {
	svc := &fakeTasks{cur: &core.TaskCurrent{}}
	code, out, errOut, opened := primeRun(svc, "-h")
	if code != 0 || errOut != "" || opened != 0 || !strings.Contains(out, "-agent string") || !strings.Contains(out, "-json") {
		t.Errorf("exit %d, stderr %q, opens %d, output %q; want 0, the flags on stdout and no open", code, errOut, opened, out)
	}
}
