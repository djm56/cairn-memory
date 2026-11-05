package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestLearningAndDirectiveAreWired checks the registry entries of `cairn learning` and `cairn
// directive`: no verb, an unknown verb and a usage error are refused with the usage, before any project
// is resolved.
func TestLearningAndDirectiveAreWired(t *testing.T) {
	t.Setenv("CAIRN_PROJECT", "")
	t.Setenv("CAIRN_SERVER", "")
	if code, _, errOut := runCmd(t, "learning"); code != 2 || !strings.Contains(errOut, "cairn learning add|list") {
		t.Errorf("cairn learning: exit %d, stderr %q; want a usage error naming the verbs", code, errOut)
	}
	if code, _, errOut := runCmd(t, "learning", "add"); code != 2 || !strings.Contains(errOut, "cairn learning add: ") {
		t.Errorf("cairn learning add with no kind: exit %d, stderr %q; want the usage error, exit 2, before any project is resolved", code, errOut)
	}
	if code, _, errOut := runCmd(t, "directive"); code != 2 || !strings.Contains(errOut, "cairn directive list") {
		t.Errorf("cairn directive: exit %d, stderr %q; want a usage error naming the verb", code, errOut)
	}
	if code, _, errOut := runCmd(t, "directive", "list", "hicks"); code != 2 || !strings.Contains(errOut, "cairn directive list: ") {
		t.Errorf("cairn directive list with a positional argument: exit %d, stderr %q; want the usage error", code, errOut)
	}
}

// TestHelpListsTheLearningAndDirectiveVerbs checks that help names the verbs of learning and directive
// in the line of each command, so a verb the registry leaves out fails here.
func TestHelpListsTheLearningAndDirectiveVerbs(t *testing.T) {
	code, out, _ := runCmd(t, "help")
	if code != 0 {
		t.Fatalf("help: exit %d", code)
	}
	want := map[string]string{
		"learning":  "learning add|list",
		"directive": "directive list",
	}
	for name, summary := range want {
		if !helpHas(out, name, summary) {
			t.Errorf("help does not list %q in the line of %s:\n%s", summary, name, out)
		}
	}
}

// TestEmptyListsAreArraysThroughTheServer runs learning list and directive list, in JSON, against a
// real server with nothing in the project. Each list must come back as [], and not null: the
// server's own answer is what a script reads, so the check is made on the wire form, not a fake.
func TestEmptyListsAreArraysThroughTheServer(t *testing.T) {
	_, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CAIRN_AGENT", "")
	for _, c := range []struct {
		key  string
		args []string
	}{
		{"learnings", []string{"learning", "list", "--json"}},
		{"directives", []string{"directive", "list", "--json"}},
	} {
		code, out, errOut := runCmd(t, c.args...)
		if code != 0 || errOut != "" {
			t.Fatalf("%v: exit %d, stderr %q", c.args, code, errOut)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(out), &obj); err != nil {
			t.Fatalf("%v: the output is not a JSON object: %v\n%s", c.args, err, out)
		}
		if got := string(obj[c.key]); got != "[]" {
			t.Errorf("%v: %s = %s, want []", c.args, c.key, got)
		}
	}
}

// TestLearningAndDirectiveThroughTheServer runs learning add and list, and directive list, against a
// real server, in JSON and as text. It checks that a proposal is stored as proposed under the agent
// named, that a task id the server does not hold is refused, and that a directive scoped to a role is
// listed for that role and not for another.
func TestLearningAndDirectiveThroughTheServer(t *testing.T) {
	local, url := newServer(t, nil)
	useDir(t, t.TempDir())
	t.Setenv("CAIRN_SERVER", url)
	t.Setenv("CAIRN_PROJECT", "acme-api")
	t.Setenv("CAIRN_AGENT", "")
	seedAgentDirective(t, local, "hicks", "Hicks reads the schema first")

	code, out, errOut := runCmd(t, "learning", "add", "--kind", "finding", "--title", "Cache the lookup",
		"--body", "The lookup runs twice.", "--agent", "hicks", "--json")
	if code != 0 || errOut != "" || strings.Count(out, "\n") != 1 {
		t.Fatalf("learning add: exit %d, stderr %q, output %q; want one line of JSON", code, errOut, out)
	}
	var added core.Learning
	if err := json.Unmarshal([]byte(out), &added); err != nil {
		t.Fatalf("learning add output is not a learning: %v\n%s", err, out)
	}
	if added.Status != core.LearningProposed || added.Agent != "hicks" || added.Title != "Cache the lookup" {
		t.Errorf("learning add = %+v, want a proposal by hicks", added)
	}

	code, out, errOut = runCmd(t, "learning", "list", "--json")
	var listed core.LearningList
	if code != 0 || errOut != "" || json.Unmarshal([]byte(out), &listed) != nil {
		t.Fatalf("learning list --json: exit %d, stderr %q, output %q", code, errOut, out)
	}
	if len(listed.Learnings) != 1 || listed.Learnings[0].ID != added.ID {
		t.Errorf("learning list = %+v, want the one proposal just added", listed.Learnings)
	}

	code, out, errOut = runCmd(t, "learning", "list")
	if code != 0 || errOut != "" || !strings.Contains(out, "Cache the lookup") {
		t.Errorf("learning list: exit %d, stderr %q, output %q; want the proposal's title", code, errOut, out)
	}

	code, _, errOut = runCmd(t, "learning", "add", "--kind", "finding", "--title", "t", "--body", "b",
		"--task-id", "task-20261105-99", "--agent", "hicks")
	if code != 1 || !strings.HasPrefix(errOut, "cairn learning add: ") {
		t.Errorf("learning add with an unknown task: exit %d, stderr %q; want exit 1 with the refusal", code, errOut)
	}

	code, out, errOut = runCmd(t, "directive", "list")
	if code != 0 || errOut != "" || !strings.Contains(out, "agent hicks") || !strings.Contains(out, "Hicks reads the schema first") {
		t.Errorf("directive list: exit %d, stderr %q, output %q; want the hicks directive", code, errOut, out)
	}
	code, out, errOut = runCmd(t, "directive", "list", "--agent", "reviewer")
	if code != 0 || errOut != "" || strings.Contains(out, "Hicks reads the schema first") || !strings.Contains(out, "no directives\n") {
		t.Errorf("directive list --agent reviewer: exit %d, stderr %q, output %q; want no hicks directive", code, errOut, out)
	}
}
