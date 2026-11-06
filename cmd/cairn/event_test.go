package main

import (
	"strings"
	"testing"
)

// TestEventIsWiredToItsVerb checks the registry entry of `cairn event`: no verb and an unknown
// verb are usage errors, and the usage is checked before any project is resolved.
func TestEventIsWiredToItsVerb(t *testing.T) {
	t.Setenv("CAIRN_PROJECT", "")
	t.Setenv("CAIRN_SERVER", "")
	if code, _, errOut := runCmd(t, "event"); code != 2 || !strings.Contains(errOut, "cairn event log") {
		t.Errorf("cairn event: exit %d, stderr %q; want a usage error naming the verb", code, errOut)
	}
	if code, _, errOut := runCmd(t, "event", "log", "task-20261030-01"); code != 2 || !strings.Contains(errOut, "cairn event log: ") {
		t.Errorf("cairn event log with no kind or agent: exit %d, stderr %q; want the usage error, exit 2, before any project is resolved", code, errOut)
	}
}
