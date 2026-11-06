package mcp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// longTask is a Service whose TaskGet returns one task with a goal of the given text, so
// the size of a full task read is set by the test.
type longTask struct {
	core.Unimplemented
	goal string
}

func (l longTask) TaskGet(context.Context, core.TaskGetRequest) (*core.TaskDetail, error) {
	return &core.TaskDetail{Task: core.Task{ID: "task-20261030-01", Title: "Long", Goal: l.goal}}, nil
}

// longList is a Service whose TaskList and TaskCurrent return one entry of the given
// text, for the refusal of a tool that is not task_get.
type longList struct {
	core.Unimplemented
	text string
}

func (l longList) TaskList(context.Context, core.TaskListRequest) (*core.TaskList, error) {
	return &core.TaskList{Tasks: []core.TaskSummary{{ID: "task-20261030-01", Title: l.text}}}, nil
}

func (l longList) TaskCurrent(context.Context) (*core.TaskCurrent, error) {
	return &core.TaskCurrent{Task: &core.Task{ID: "task-20261030-01", Title: "Long", Goal: l.text}}, nil
}

// TestLongResultIsAToolErrorNamingTheArguments checks #111: a task read with no section
// that would send more than maxResultBytes is a tool error, not the whole task. The
// message names the section argument, and the limit and cursor arguments that page the
// events, so the model can narrow the read; the error itself is small. Removing the
// ceiling makes this test fail.
func TestLongResultIsAToolErrorNamingTheArguments(t *testing.T) {
	cs := connect(t, longTask{goal: strings.Repeat("a", 80<<10)}, acme)
	text, isErr := call(t, cs, "task_get", map[string]any{"id": "task-20261030-01"})
	got := errorOf(t, text, isErr)
	if got.Kind != "invalid" {
		t.Fatalf("a long task read: kind %q, want invalid (%s)", got.Kind, text)
	}
	for _, want := range []string{"section", "limit", "cursor"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("the message %q does not name the %s argument", got.Message, want)
		}
	}
	if len(text) > 4<<10 {
		t.Errorf("the tool error is %d bytes; the refusal should be small", len(text))
	}
}

// TestLongListNamesOnlyTheArgumentsItTakes checks #111 for a tool other than task_get:
// task_list takes status, limit and cursor, and not section, so its refusal must name
// those and must not name section. Under the old shared message it did.
func TestLongListNamesOnlyTheArgumentsItTakes(t *testing.T) {
	cs := connect(t, longList{text: strings.Repeat("a", 80<<10)}, acme)
	text, isErr := call(t, cs, "task_list", map[string]any{})
	got := errorOf(t, text, isErr)
	if got.Kind != "invalid" {
		t.Fatalf("a long list: kind %q, want invalid (%s)", got.Kind, text)
	}
	for _, want := range []string{"status", "limit", "cursor"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("the message %q does not name the %s argument", got.Message, want)
		}
	}
	if strings.Contains(got.Message, "section") {
		t.Errorf("the message %q names the section argument, which task_list does not take", got.Message)
	}
}

// TestLongResultOfToolWithNoNarrowingArgument checks #111 for a tool with no argument
// that narrows its result: task_current takes only agent. Its refusal is the generic
// clause, which names no argument at all.
func TestLongResultOfToolWithNoNarrowingArgument(t *testing.T) {
	cs := connect(t, longList{text: strings.Repeat("a", 80<<10)}, acme)
	text, isErr := call(t, cs, "task_current", map[string]any{})
	got := errorOf(t, text, isErr)
	if got.Kind != "invalid" {
		t.Fatalf("a long current task: kind %q, want invalid (%s)", got.Kind, text)
	}
	for _, not := range []string{"section", "limit", "cursor", "status"} {
		if strings.Contains(got.Message, not) {
			t.Errorf("the message %q names the %s argument, which task_current does not take", got.Message, not)
		}
	}
}

// TestResultUnderTheCeilingIsAnswered checks that a task read under the ceiling is
// answered as before, with the goal in it.
func TestResultUnderTheCeilingIsAnswered(t *testing.T) {
	goal := strings.Repeat("b", 1<<10)
	cs := connect(t, longTask{goal: goal}, acme)
	text, isErr := call(t, cs, "task_get", map[string]any{"id": "task-20261030-01"})
	if isErr {
		t.Fatalf("a short task read returned a tool error: %s", text)
	}
	if !strings.Contains(text, goal) {
		t.Errorf("the task read does not carry its goal (%d bytes of answer)", len(text))
	}
}

// TestResultAtTheCeilingIsAnsweredAndOneMoreIsRefused checks the boundary of #111 at 64 KiB:
// an answer of exactly that many bytes is answered, and one of a byte more is a tool error.
// The test sets the size of the answer from a probe: the answer grows one byte for each byte
// of goal, so a goal of one byte gives the base size, and the goal that makes the answer the
// ceiling is found from it. The probe's size is checked, so the test cannot pass on a size
// it did not set. A check of > written as >= would fail the first half here.
func TestResultAtTheCeilingIsAnsweredAndOneMoreIsRefused(t *testing.T) {
	const ceiling = 64 << 10
	probe, isErr := call(t, connect(t, longTask{goal: "a"}, acme), "task_get", map[string]any{"id": "task-20261030-01"})
	if isErr {
		t.Fatalf("the probe read returned a tool error: %s", probe)
	}
	goal := strings.Repeat("a", ceiling-len(probe)+1)

	text, isErr := call(t, connect(t, longTask{goal: goal}, acme), "task_get", map[string]any{"id": "task-20261030-01"})
	if isErr {
		t.Fatalf("an answer of exactly %d bytes was refused: %s", ceiling, text)
	}
	if len(text) != ceiling {
		t.Fatalf("the answer is %d bytes, want exactly %d: the probe did not set the size", len(text), ceiling)
	}

	over, isErr := call(t, connect(t, longTask{goal: goal + "a"}, acme), "task_get", map[string]any{"id": "task-20261030-01"})
	got := errorOf(t, over, isErr)
	if got.Kind != "invalid" {
		t.Errorf("an answer of %d bytes: kind %q, want invalid (%s)", ceiling+1, got.Kind, over)
	}
}
