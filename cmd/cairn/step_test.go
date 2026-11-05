package main

import (
	"strings"
	"testing"
)

// TestStepIsWiredToItsVerbs checks the registry entry of `cairn step`: no verb and an unknown verb
// are usage errors that name the verbs, and the usage is checked before any project is resolved.
func TestStepIsWiredToItsVerbs(t *testing.T) {
	t.Setenv("CAIRN_PROJECT", "")
	t.Setenv("CAIRN_SERVER", "")
	if code, _, errOut := runCmd(t, "step"); code != 2 || !strings.Contains(errOut, "cairn step add|update") {
		t.Errorf("cairn step: exit %d, stderr %q; want a usage error naming the verbs", code, errOut)
	}
	if code, _, errOut := runCmd(t, "step", "add", "task-20261030-01"); code != 2 || !strings.Contains(errOut, "cairn step add: ") {
		t.Errorf("cairn step add with no title or agent: exit %d, stderr %q; want the usage error, exit 2, before any project is resolved", code, errOut)
	}
}

// TestHelpListsTheWriteCommands checks that help names the step command beside task.
func TestHelpListsTheWriteCommands(t *testing.T) {
	code, out, _ := runCmd(t, "help")
	if code != 0 || !strings.Contains(out, "step") || !strings.Contains(out, "start") {
		t.Errorf("help: exit %d, output %q; want the step command and task start listed", code, out)
	}
}
