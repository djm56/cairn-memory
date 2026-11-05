package cli

import (
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestTaskCurrentCapsADirectiveText checks that the title and the rule of a directive are cut to one line
// through task current, as the directive list cuts them: a text longer than MaxLineRunes is cut to that
// many runes, the last of them an ellipsis. The code is left as it is, since the server sets it.
func TestTaskCurrentCapsADirectiveText(t *testing.T) {
	task := activeTask()
	long := strings.Repeat("a", MaxLineRunes+50)
	capped := strings.Repeat("a", MaxLineRunes-1) + "…"
	svc := &fakeTasks{cur: &core.TaskCurrent{
		Task: &task, Steps: twoSteps(),
		Directives: []core.Directive{{Code: "D-0001", Scope: core.ScopeProject, Title: long, Rule: long}},
	}}
	code, out, errOut := runTask(t, svc, "current")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing on stderr", code, errOut)
	}
	for _, want := range []string{
		"  D-0001  project  " + capped + "\n",
		"          " + capped + "\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("task current lacks the capped line %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, long) {
		t.Errorf("task current prints a directive text whole, past the cap:\n%s", out)
	}
}
