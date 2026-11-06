package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRecallIsWired checks the registry entry of `cairn recall`: a missing goal and a positional
// argument are usage errors, exit 2, before any project is resolved.
func TestRecallIsWired(t *testing.T) {
	t.Setenv("CAIRN_PROJECT", "")
	t.Setenv("CAIRN_SERVER", "")
	if code, _, errOut := runCmd(t, "recall"); code != 2 || !strings.Contains(errOut, "cairn recall: --goal is required") {
		t.Errorf("cairn recall: exit %d, stderr %q; want the usage error naming --goal", code, errOut)
	}
	if code, _, errOut := runCmd(t, "recall", "--goal", "g", "hicks"); code != 2 || !strings.Contains(errOut, "cairn recall: takes no positional argument") {
		t.Errorf("cairn recall with a positional argument: exit %d, stderr %q; want the usage error", code, errOut)
	}
}

// TestHelpListsRecall checks that help names the flags of recall in the line of the command.
func TestHelpListsRecall(t *testing.T) {
	code, out, _ := runCmd(t, "help")
	if code != 0 {
		t.Fatalf("help: exit %d", code)
	}
	if !helpHas(out, "recall", "--goal") {
		t.Errorf("help does not list --goal in the line of recall:\n%s", out)
	}
}

// TestRecallThroughTheServer runs recall against a real server. A task and an agent-scoped directive
// are seeded. The task is found from any caller, the directive only when the caller's role names it,
// which CAIRN_AGENT does when --agent is left out.
func TestRecallThroughTheServer(t *testing.T) {
	local, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CAIRN_AGENT", "")
	taskID := seedNamedTask(t, local, "acme-api", "Wire the export", "Read the schema first")
	seedAgentDirective(t, local, "hicks", "Hicks reads the schema first")

	code, out, errOut := runCmd(t, "recall", "--goal", "export the task")
	if code != 0 || errOut != "" || !strings.Contains(out, "Wire the export") || strings.Contains(out, "Hicks reads the schema first") {
		t.Errorf("recall with no role: exit %d, stderr %q, output %q; want the task and no hicks directive", code, errOut, out)
	}

	t.Setenv("CAIRN_AGENT", "hicks")
	code, out, errOut = runCmd(t, "recall", "--goal", "export the task")
	if code != 0 || errOut != "" || !strings.Contains(out, "Hicks reads the schema first") {
		t.Errorf("recall with CAIRN_AGENT=hicks: exit %d, stderr %q, output %q; want the hicks directive", code, errOut, out)
	}

	code, out, errOut = runCmd(t, "recall", "--goal", "export the task", "--json")
	var got struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	if code != 0 || errOut != "" || json.Unmarshal([]byte(out), &got) != nil || len(got.Tasks) != 1 || got.Tasks[0].ID != taskID {
		t.Errorf("recall --json: exit %d, stderr %q, output %q; want the one task", code, errOut, out)
	}
}

// TestRecallEmptyThroughTheServer checks that a recall with nothing to find writes every part as [] in
// JSON, through a real server, so the arrays are not nil on the wire.
func TestRecallEmptyThroughTheServer(t *testing.T) {
	_, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CAIRN_AGENT", "")
	code, out, errOut := runCmd(t, "recall", "--goal", "nothing matches this", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("the output is not a JSON object: %v\n%s", err, out)
	}
	for _, key := range []string{"directives", "lessons", "mistakes", "tasks"} {
		if got := string(obj[key]); got != "[]" {
			t.Errorf("%s = %s, want []", key, got)
		}
	}
}
