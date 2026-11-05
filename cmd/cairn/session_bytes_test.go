package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// sessionBlockCase is one input of the session-start block, with the exact bytes the block printed
// before the block moved out of this package. A change to any byte of them is a change to what the
// hook gives an agent, and the test fails on it.
type sessionBlockCase struct {
	name    string
	cur     *core.TaskCurrent
	role    string
	refused bool
	want    []string
}

// sessionBlockCases returns the inputs the byte test runs. Each want is the block's lines, each with
// its newline.
func sessionBlockCases() []sessionBlockCase {
	active := &core.TaskCurrent{
		Task: &core.Task{
			ID: "task-20261031-01", Title: "Ship the export", Status: core.TaskActive,
			NextAction: "Read the schema first",
		},
		Steps: []core.Step{
			{Number: 1, Title: "Read the schema", Status: core.StepActive},
			{Number: 2, Title: "Write the printer", Status: core.StepPending},
		},
		Directives:  []core.Directive{{Code: "D-0001"}, {Code: "D-0002"}},
		Expired:     2,
		Omitted:     3,
		OtherActive: 1,
	}
	return []sessionBlockCase{
		{
			name: "no task",
			cur:  &core.TaskCurrent{},
			want: []string{
				"Cairn: no task is active in acme-api.",
			},
		},
		{
			name: "an active task with a role, and the counts",
			cur:  active,
			role: "hicks",
			want: []string{
				`Cairn task: "Ship the export" (task-20261031-01, active) in acme-api.`,
				"Next action: Read the schema first",
				`Current step: 1 of 2, "Read the schema" (active).`,
				"Other active tasks: 1.",
				"Directives that apply: 2.",
				"Directives expired and left out (past review_by): 2.",
				"Directives omitted (beyond the cap of 12): 3. Call directive_list for all.",
				"Call task_current for the brief, the steps and recent events.",
			},
		},
		{
			name: "an active task with no role",
			cur:  active,
			want: []string{
				`Cairn task: "Ship the export" (task-20261031-01, active) in acme-api.`,
				"Next action: Read the schema first",
				`Current step: 1 of 2, "Read the schema" (active).`,
				"Other active tasks: 1.",
				"Directives that apply: 2.",
				"Agent-scoped directives were not counted: no agent role is set.",
				"Directives expired and left out (past review_by): 2.",
				"Directives omitted (beyond the cap of 12): 3. Call directive_list for all.",
				"Call task_current for the brief, the steps and recent events.",
			},
		},
		{
			name:    "an active task with a refused variable",
			cur:     &core.TaskCurrent{Task: &core.Task{ID: "task-20261031-01", Title: "Ship", Status: core.TaskBlocked}},
			refused: true,
			want: []string{
				`Cairn task: "Ship" (task-20261031-01, blocked) in acme-api.`,
				"Next action: none recorded.",
				"Current step: none open.",
				"Directives that apply: 0.",
				refusedRoleNote,
				"Call task_current for the brief, the steps and recent events.",
			},
		},
	}
}

// TestSessionStartBlockBytesDoNotChange checks the session-start block byte for byte, for each input
// in sessionBlockCases. It is the pin on the refactor that moves the block: the hook prints the same
// bytes for the same input before and after.
func TestSessionStartBlockBytesDoNotChange(t *testing.T) {
	for _, c := range sessionBlockCases() {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			writeSessionStart(&out, "acme-api", c.cur, c.role, c.refused)
			want := strings.Join(c.want, "\n") + "\n"
			if out.String() != want {
				t.Errorf("session-start block:\n%q\nwant:\n%q", out.String(), want)
			}
		})
	}
}
