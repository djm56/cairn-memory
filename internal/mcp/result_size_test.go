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

// TestLongResultIsAToolErrorNamingTheArguments checks #111: a task read with no section
// that would send more than maxResultBytes is a tool error, not the whole task. The
// message names the section and limit arguments, so the model can narrow the read, and
// the error itself is small. Removing the ceiling makes this test fail.
func TestLongResultIsAToolErrorNamingTheArguments(t *testing.T) {
	cs := connect(t, longTask{goal: strings.Repeat("a", 80<<10)}, acme)
	text, isErr := call(t, cs, "task_get", map[string]any{"id": "task-20261030-01"})
	got := errorOf(t, text, isErr)
	if got.Kind != "invalid" {
		t.Fatalf("a long task read: kind %q, want invalid (%s)", got.Kind, text)
	}
	for _, want := range []string{"section", "limit"} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("the message %q does not name the %s argument", got.Message, want)
		}
	}
	if len(text) > 4<<10 {
		t.Errorf("the tool error is %d bytes; the refusal should be small", len(text))
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
