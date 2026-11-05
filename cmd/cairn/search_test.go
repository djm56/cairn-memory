package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSearchIsWired checks the registry entry of `cairn search`: a missing query and an unknown kind are
// usage errors, exit 2, before any project is resolved.
func TestSearchIsWired(t *testing.T) {
	t.Setenv("CAIRN_PROJECT", "")
	t.Setenv("CAIRN_SERVER", "")
	if code, _, errOut := runCmd(t, "search"); code != 2 || !strings.Contains(errOut, "cairn search: --query is required") {
		t.Errorf("cairn search: exit %d, stderr %q; want the usage error naming --query", code, errOut)
	}
	if code, _, errOut := runCmd(t, "search", "--query", "g", "--kind", "idea"); code != 2 || !strings.Contains(errOut, "cairn search: --kind must be task, event") {
		t.Errorf("cairn search with an unknown kind: exit %d, stderr %q; want the usage error, exit 2, before any project is resolved", code, errOut)
	}
}

// TestHelpListsSearch checks that help names the flags of search in the line of the command.
func TestHelpListsSearch(t *testing.T) {
	code, out, _ := runCmd(t, "help")
	if code != 0 {
		t.Fatalf("help: exit %d", code)
	}
	if !helpHas(out, "search", "--query") {
		t.Errorf("help does not list --query in the line of search:\n%s", out)
	}
}

// TestSearchThroughTheServer runs search against a real server. A task and an agent-scoped directive are
// seeded. A query finds the task, a query on the directive's words finds the directive by its code, and
// a kind that holds none of the words finds nothing.
func TestSearchThroughTheServer(t *testing.T) {
	local, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CAIRN_AGENT", "")
	seedNamedTask(t, local, "acme-api", "Wire the export", "Read the schema first")
	seedAgentDirective(t, local, "hicks", "Hicks reads the schema first")

	code, out, errOut := runCmd(t, "search", "--query", "export")
	if code != 0 || errOut != "" || !strings.Contains(out, "task  task-") || !strings.Contains(out, "Wire the export") {
		t.Errorf("search export: exit %d, stderr %q, output %q; want the task hit", code, errOut, out)
	}

	code, out, errOut = runCmd(t, "search", "--query", "schema", "--kind", "directive", "--json")
	var got struct {
		Hits []struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"hits"`
	}
	if code != 0 || errOut != "" || json.Unmarshal([]byte(out), &got) != nil || len(got.Hits) != 1 ||
		got.Hits[0].Kind != "directive" || !strings.HasPrefix(got.Hits[0].ID, "D-") {
		t.Errorf("search schema --kind directive --json: exit %d, stderr %q, output %q; want one directive hit named by its code", code, errOut, out)
	}

	code, out, errOut = runCmd(t, "search", "--query", "export", "--kind", "lesson")
	if code != 0 || errOut != "" || out != "no hits\n" {
		t.Errorf("search export --kind lesson: exit %d, stderr %q, output %q; want the no-hit line", code, errOut, out)
	}
}

// TestSearchEmptyThroughTheServer checks that a search with no hit writes hits as [] in JSON, through a
// real server, so the array is not nil on the wire.
func TestSearchEmptyThroughTheServer(t *testing.T) {
	_, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CAIRN_AGENT", "")
	code, out, errOut := runCmd(t, "search", "--query", "nothing matches this", "--json")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("the output is not a JSON object: %v\n%s", err, out)
	}
	if got := string(obj["hits"]); got != "[]" {
		t.Errorf("hits = %s, want []", got)
	}
}
