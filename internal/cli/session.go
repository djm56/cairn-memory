package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/djm56/cairn-memory/internal/core"
)

// NoRoleNote is the sentence status prints under its directive count, and the session-start block
// prints, when no agent role is set. With no role the server counts only the directives not scoped to
// one, and the sentence says so.
const NoRoleNote = "Agent-scoped directives were not counted: no agent role is set."

// RefusedRoleNote is the sentence status prints, and the session-start block prints, when CAIRN_AGENT
// is set but the wire form refuses it. It names the variable and the reason, so a person does not read
// it as the variable being unset.
const RefusedRoleNote = "Agent-scoped directives were not counted: CAIRN_AGENT is set but refused (a control character or a space at either end)."

// HookPointer is the closing line of the session-start block the hook prints. It names the MCP tool an
// agent with the server's tools calls for the brief.
const HookPointer = "Call task_current for the brief, the steps and recent events."

// HookOmittedCall is the call the omitted-directives line of the hook's block names. The hook's agent has
// the server's tools, so the call is the MCP tool.
const HookOmittedCall = "directive_list"

// PrimePointer is the closing line of the session-start block prime prints. Prime writes the summary of
// the task directly after the block, so the pointer says that the brief, steps and events follow, and it
// names the one CLI command the summary does not show.
const PrimePointer = "The brief, steps and recent events follow; run cairn directive list for all directives."

// PrimeOmittedCall is the call the omitted-directives line of prime's block names. Prime's agent has only
// a shell, so the call is the CLI command.
const PrimeOmittedCall = "cairn directive list"

// SessionStart writes the session-start block for cur: the active task, its next action and current
// step, the count of directives that apply, and the closing line pointer. With no active task it is one
// line. The hook and prime both write it, so the two cannot drift; they differ only in the wording that
// names a call: the pointer and the call the omitted-directives line names.
//
// role is the agent role the counts were read for; with none, the block says agent-scoped directives
// were not counted. refused is true when CAIRN_AGENT is set but refused: the block then names the
// variable as the reason, so the agent is not told that no role is set when one was given.
func SessionStart(w io.Writer, project string, cur *core.TaskCurrent, role string, refused bool, pointer, omittedCall string) {
	if cur.Task == nil {
		fmt.Fprintf(w, "Cairn: no task is active in %s.\n", project)
		return
	}
	// Every server-derived text goes through Plain: the block is read by a model, so a title or next
	// action cannot end its line or start one of its own.
	task := cur.Task
	fmt.Fprintf(w, "Cairn task: \"%s\" (%s, %s) in %s.\n", Plain(task.Title), Plain(task.ID), Plain(string(task.Status)), project)
	if next := Plain(task.NextAction); next != "" {
		fmt.Fprintf(w, "Next action: %s\n", next)
	} else {
		fmt.Fprintln(w, "Next action: none recorded.")
	}
	if s := currentStep(cur.Steps); s != nil {
		fmt.Fprintf(w, "Current step: %d of %d, \"%s\" (%s).\n", s.Number, len(cur.Steps), Plain(s.Title), Plain(string(s.Status)))
	} else {
		fmt.Fprintln(w, "Current step: none open.")
	}
	if cur.OtherActive > 0 {
		fmt.Fprintf(w, "Other active tasks: %d.\n", cur.OtherActive)
	}
	fmt.Fprintf(w, "Directives that apply: %d.\n", len(cur.Directives))
	// The same note as cairn status: with no role, no agent-scoped directive is counted.
	if role == "" {
		if refused {
			fmt.Fprintln(w, RefusedRoleNote)
		} else {
			fmt.Fprintln(w, NoRoleNote)
		}
	}
	// The agent reads this block, so a directive left out is said so, with the reason.
	if cur.Expired > 0 {
		fmt.Fprintf(w, "Directives expired and left out (past review_by): %d.\n", cur.Expired)
	}
	if cur.Omitted > 0 {
		fmt.Fprintf(w, "Directives omitted (beyond the cap of %d): %d. Call %s for all.\n", core.DirectiveCap, cur.Omitted, omittedCall)
	}
	fmt.Fprintln(w, pointer)
}

// StepWarning writes the one-line warning the stop hook prints: a step of the active task that is active
// with no summary. Only two fields are read, the step's status and its summary. A step is named when its
// status is active and its summary is blank once trimmed. The summary is the latest result, and a step that
// starts again after a failure keeps the summary of its failed attempt, so such a step is not named; the
// fields do not say which attempt a summary belongs to.
//
// With one such step the warning names it. With more, it gives the count and names the first. It writes
// nothing when no task is active or when every active step has a summary.
func StepWarning(w io.Writer, cur *core.TaskCurrent) {
	if cur.Task == nil {
		return
	}
	n := 0
	first := -1
	for i, s := range cur.Steps {
		if s.Status == core.StepActive && strings.TrimSpace(s.Summary) == "" {
			if first < 0 {
				first = i
			}
			n++
		}
	}
	if n == 0 {
		return
	}
	s := cur.Steps[first]
	// The title is server text, so it goes through Plain: it cannot end the line or start one.
	if n == 1 {
		fmt.Fprintf(w, "Cairn warning: step %d of %d, \"%s\", is active with no summary. Record a summary with step_update when it is done.\n", s.Number, len(cur.Steps), Plain(s.Title))
		return
	}
	fmt.Fprintf(w, "Cairn warning: %d steps are active with no summary; the first is step %d of %d, \"%s\". Record a summary with step_update when each is done.\n", n, s.Number, len(cur.Steps), Plain(s.Title))
}
