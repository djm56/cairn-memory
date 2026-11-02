package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestStatusAndHookSayNoRoleWasCounted checks 14-W4. The CLI's caller names no agent role, and
// identity supplies none, so the server counts only directives that are not scoped to a role. The
// status block and the session-start block both say that agent-scoped directives were not counted,
// so the count is not read as the whole of what applies.
func TestStatusAndHookSayNoRoleWasCounted(t *testing.T) {
	cur := countsTask()

	var status bytes.Buffer
	writeStatus(&status, &target{}, cur)
	if !strings.Contains(status.String(), "            "+noRoleNote+"\n") {
		t.Errorf("cairn status does not say agent-scoped directives were not counted:\n%s", status.String())
	}

	var hook bytes.Buffer
	writeSessionStart(&hook, "acme-api", cur)
	if !strings.Contains(hook.String(), "Agent-scoped directives were not counted: no agent role is set.\n") {
		t.Errorf("the session-start hook does not say agent-scoped directives were not counted:\n%s", hook.String())
	}
}
