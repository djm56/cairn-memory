package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// stepWarningOf returns what StepWarning writes for cur.
func stepWarningOf(cur *core.TaskCurrent) string {
	var b bytes.Buffer
	if err := StepWarning(&b, cur); err != nil {
		return "error: " + err.Error()
	}
	return b.String()
}

// warningJSON is the line StepWarning writes for the warning msg: one JSON object whose only field is
// systemMessage, with its newline.
func warningJSON(msg string) string {
	b, _ := json.Marshal(map[string]string{"systemMessage": msg})
	return string(b) + "\n"
}

// TestStepWarningNamesAnActiveStepWithNoSummary checks the warning for the active step of activeTask, which
// has no summary: one JSON object whose systemMessage gives its number, its count, its title, and the routes
// that record a summary.
func TestStepWarningNamesAnActiveStepWithNoSummary(t *testing.T) {
	task := activeTask()
	cur := &core.TaskCurrent{Task: &task, Steps: twoSteps()}
	want := warningJSON(`Cairn warning: step 1 of 2, "Write test", is active with no summary. Record a summary with cairn step update or the step_update tool when it is done.`)
	if got := stepWarningOf(cur); got != want {
		t.Errorf("warning = %q\nwant %q", got, want)
	}
}

// TestStepWarningTreatsABlankSummaryAsNone checks that a summary of spaces and tabs is no summary.
func TestStepWarningTreatsABlankSummaryAsNone(t *testing.T) {
	task := activeTask()
	steps := twoSteps()
	steps[0].Summary = " \t "
	if got := stepWarningOf(&core.TaskCurrent{Task: &task, Steps: steps}); got == "" {
		t.Errorf("an active step with a blank summary is not named")
	}
}

// TestStepWarningIsSilentWhenTheActiveStepHasASummary checks that an active step with a summary is not
// named, and that nothing is written.
func TestStepWarningIsSilentWhenTheActiveStepHasASummary(t *testing.T) {
	task := activeTask()
	steps := twoSteps()
	steps[0].Summary = "Tests written."
	if got := stepWarningOf(&core.TaskCurrent{Task: &task, Steps: steps}); got != "" {
		t.Errorf("warning = %q, want nothing: the active step has a summary", got)
	}
}

// TestStepWarningReadsOnlyActiveSteps checks that a step which is not active is never named, whatever its
// summary: pending, done, failed and skipped steps print nothing.
func TestStepWarningReadsOnlyActiveSteps(t *testing.T) {
	task := activeTask()
	for _, status := range []core.StepStatus{core.StepPending, core.StepDone, core.StepFailed, core.StepSkipped} {
		steps := []core.Step{{Number: 1, Title: "Write test", Status: status}}
		if got := stepWarningOf(&core.TaskCurrent{Task: &task, Steps: steps}); got != "" {
			t.Errorf("a %s step with no summary: warning = %q, want nothing", status, got)
		}
	}
}

// TestStepWarningIsSilentWithNoTask checks that with no active task nothing is written, since there is no
// step to name.
func TestStepWarningIsSilentWithNoTask(t *testing.T) {
	if got := stepWarningOf(&core.TaskCurrent{}); got != "" {
		t.Errorf("warning = %q with no task, want nothing", got)
	}
}

// TestStepWarningCountsSeveralActiveSteps checks that with more than one active step with no summary, the
// warning gives the count and names the first, and that an active step with a summary is not counted.
func TestStepWarningCountsSeveralActiveSteps(t *testing.T) {
	task := activeTask()
	steps := []core.Step{
		{Number: 1, Title: "Write test", Status: core.StepActive},
		{Number: 2, Title: "Implement", Status: core.StepActive},
		{Number: 3, Title: "Document", Status: core.StepActive, Summary: "Done."},
	}
	want := warningJSON(`Cairn warning: 2 steps are active with no summary; the first is step 1 of 3, "Write test". Record a summary with cairn step update or the step_update tool when each is done.`)
	if got := stepWarningOf(&core.TaskCurrent{Task: &task, Steps: steps}); got != want {
		t.Errorf("warning = %q\nwant %q", got, want)
	}
}

// TestStepWarningFoldsTheTitle checks that a title with a line feed and an escape sequence, built from
// code points, is one line in the warning, and the sequence goes whole. The object's bytes carry no escape
// character and no carriage return: the title cannot reach the client as a control character.
func TestStepWarningFoldsTheTitle(t *testing.T) {
	task := activeTask()
	steps := []core.Step{{Number: 1, Title: "Write" + string(rune(0x0a)) + "test" + string(rune(0x1b)) + "[31m now", Status: core.StepActive}}
	want := warningJSON(`Cairn warning: step 1 of 1, "Write test now", is active with no summary. Record a summary with cairn step update or the step_update tool when it is done.`)
	got := stepWarningOf(&core.TaskCurrent{Task: &task, Steps: steps})
	if got != want {
		t.Errorf("warning = %q\nwant %q", got, want)
	}
	if strings.ContainsAny(got, "\x1b\r") {
		t.Errorf("warning carries a control character: %q", got)
	}
}
