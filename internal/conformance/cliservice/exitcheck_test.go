package cliservice

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/core"
)

// exitScript is the agent of the exit check: a bash script with nothing but cairn commands and bash
// builtins. It starts a task, runs one step, logs an event and closes the task with its criteria
// results. Its server, key, project and agent come from the environment, so no flag names them.
// Each cairn call's output is read by bash's own pattern match, so no other program is needed.
const exitScript = `#!/usr/bin/env bash
set -euo pipefail

started=$(cairn task start --title "Exit check" --goal "Close a task through the CLI with only bash" --json-input --json <<'JSON'
{"criteria": ["The task closes done"]}
JSON
)
if [[ ! $started =~ \"id\":\"(task-[0-9]+-[0-9]+)\" ]]; then
  echo "no task id in the start answer: $started" >&2
  exit 1
fi
task="${BASH_REMATCH[1]}"

cairn step add "$task" --title "Run the check" --json > /dev/null
cairn step update "$task" 1 --action start --json > /dev/null
cairn step update "$task" 1 --action finish --summary "The check ran." --json > /dev/null
cairn event log "$task" --kind note --text "Checked the step by hand." --json > /dev/null
cairn task close "$task" --outcome done --summary "The check passed." --no-lessons-reason "Nothing new to record." --json-input --json > /dev/null <<'JSON'
{"criteria": [{"ord": 1, "result": "met", "evidence": "the step and the close ran"}], "lessons": [], "mistakes": []}
JSON
printf '%s\n' "$task"
`

// exitProject is the project the exit check's task is in.
const exitProject = "exit-check"

// TestExitCheckAgentWithOnlyBashCompletesAndClosesATask is M4's exit check: an agent with only bash
// completes and closes a task through the CLI. The agent is exitScript, run by bash with a PATH that
// holds only the cairn binary, so a command it runs that is not cairn is not found and fails the run.
// The server is an in-process loopback server over a real core, and the task is read back through the
// API to show it closed done.
func TestExitCheckAgentWithOnlyBashCompletesAndClosesATask(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not on PATH, so the exit check cannot run: install bash to run it")
	}
	local := newLocal(t)
	srv := httptest.NewServer(api.Handler(local, api.UnauthenticatedHeaderCaller, nil))
	t.Cleanup(srv.Close)

	// The PATH holds one directory, with cairn in it and nothing else.
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cairnBin, filepath.Join(binDir, "cairn")); err != nil {
		t.Fatalf("link cairn into the PATH: %v", err)
	}
	script := filepath.Join(dir, "agent.sh")
	if err := os.WriteFile(script, []byte(exitScript), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bash, script)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + binDir,
		"HOME=" + dir,
		"CAIRN_SERVER=" + srv.URL,
		"CAIRN_API_KEY=" + testKey,
		"CAIRN_PROJECT=" + exitProject,
		"CAIRN_AGENT=main",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("the bash agent failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	taskID := strings.TrimSpace(stdout.String())
	if !strings.HasPrefix(taskID, "task-") {
		t.Fatalf("the agent printed %q as its task id", taskID)
	}

	hc, err := client.NewWithKey(srv.URL, testKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := core.WithCaller(context.Background(), core.Caller{Project: exitProject, Agent: "main", Role: core.RoleAgent})
	brief, err := hc.TaskGet(ctx, core.TaskGetRequest{ID: taskID, Section: core.SectionBrief})
	if err != nil {
		t.Fatalf("read the task through the API: %v", err)
	}
	if brief.Task.Status != core.TaskDone {
		t.Errorf("task %s is %s through the API, want done", taskID, brief.Task.Status)
	}
	if brief.Task.OutcomeSummary != "The check passed." {
		t.Errorf("outcome summary = %q, want the one the agent closed with", brief.Task.OutcomeSummary)
	}
	steps, err := hc.TaskGet(ctx, core.TaskGetRequest{ID: taskID, Section: core.SectionSteps})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps.Steps) != 1 || steps.Steps[0].Status != core.StepDone {
		t.Errorf("steps = %+v, want the one step done", steps.Steps)
	}
	events, err := hc.TaskGet(ctx, core.TaskGetRequest{ID: taskID, Section: core.SectionEvents})
	if err != nil {
		t.Fatal(err)
	}
	var noted bool
	for _, e := range events.Events {
		if e.Kind == core.EventNote && e.Text == "Checked the step by hand." {
			noted = true
		}
	}
	if !noted {
		t.Errorf("the event the agent logged is not in the task's journal: %+v", events.Events)
	}
}
