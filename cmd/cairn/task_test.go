package main

import (
	"strings"
	"testing"
)

// TestTaskIsWiredToItsVerbs checks the registry entry of `cairn task`: no verb and an unknown
// verb are usage errors that name the verbs, so the command reaches internal/cli.
func TestTaskIsWiredToItsVerbs(t *testing.T) {
	if code, _, errOut := runCmd(t, "task"); code != 2 || !strings.Contains(errOut, "cairn task current|get|list") {
		t.Errorf("cairn task: exit %d, stderr %q; want a usage error naming the verbs", code, errOut)
	}
	if code, _, errOut := runCmd(t, "task", "frobnicate"); code != 2 || !strings.Contains(errOut, `unknown verb "frobnicate"`) {
		t.Errorf("cairn task frobnicate: exit %d, stderr %q; want an unknown-verb usage error", code, errOut)
	}
}

// TestTaskUsageIsCheckedBeforeTheProject checks that a usage error needs no project and no
// server: the command refuses the extra id before it resolves either, so the exit is 2, not 1.
func TestTaskUsageIsCheckedBeforeTheProject(t *testing.T) {
	t.Setenv("CAIRN_PROJECT", "")
	t.Setenv("CAIRN_SERVER", "")
	code, _, errOut := runCmd(t, "task", "current", "task-20261030-01")
	if code != 2 || !strings.Contains(errOut, "takes no task id") {
		t.Errorf("exit %d, stderr %q; want the usage error, exit 2, before any project is resolved", code, errOut)
	}
}
