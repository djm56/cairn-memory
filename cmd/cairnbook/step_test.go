package main

import (
	"strings"
	"testing"
)

// TestStepIsWiredToItsVerbs checks the registry entry of `cairnbook step`: no verb and an unknown verb
// are usage errors that name the verbs, and the usage is checked before any project is resolved.
func TestStepIsWiredToItsVerbs(t *testing.T) {
	t.Setenv("CAIRNBOOK_PROJECT", "")
	t.Setenv("CAIRNBOOK_SERVER", "")
	if code, _, errOut := runCmd(t, "step"); code != 2 || !strings.Contains(errOut, "cairnbook step add|update") {
		t.Errorf("cairnbook step: exit %d, stderr %q; want a usage error naming the verbs", code, errOut)
	}
	if code, _, errOut := runCmd(t, "step", "add", "task-20261030-01"); code != 2 || !strings.Contains(errOut, "cairnbook step add: ") {
		t.Errorf("cairnbook step add with no title or agent: exit %d, stderr %q; want the usage error, exit 2, before any project is resolved", code, errOut)
	}
}

// TestHelpListsTheWriteCommands checks that help names every verb of the task, step and event
// commands, in the line of its command. A verb the registry leaves out fails here, so help cannot
// hide one that exists.
func TestHelpListsTheWriteCommands(t *testing.T) {
	code, out, _ := runCmd(t, "help")
	if code != 0 {
		t.Fatalf("help: exit %d", code)
	}
	want := map[string]string{
		"task":  "task current|get|list|start|update|close",
		"step":  "step add|update|plan",
		"event": "event log",
	}
	for name, summary := range want {
		if !helpHas(out, name, summary) {
			t.Errorf("help does not list %q in the line of %s:\n%s", summary, name, out)
		}
	}
}

// helpHas reports whether the help line of the command name holds want. A help line is the
// command's name padded to a column, then its summary.
func helpHas(out, name, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == name {
			return strings.Contains(line, want)
		}
	}
	return false
}
