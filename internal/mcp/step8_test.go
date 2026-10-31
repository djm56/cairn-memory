package mcp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	cairnmcp "github.com/djm56/cairn-memory/internal/mcp"
)

// panicking is a Service whose TaskCurrent panics. Every other call is unimplemented.
type panicking struct{ core.Unimplemented }

func (panicking) TaskCurrent(context.Context) (*core.TaskCurrent, error) {
	panic("nil map write")
}

// TestAnswersKeepMarkupCharacters is step 8 review S1. A result is read by a model,
// and "&&", "->" and "<" are text, not escapes. The goal goes back as it was written.
func TestAnswersKeepMarkupCharacters(t *testing.T) {
	svc, _ := newService(t)
	cs := connect(t, svc, acme)
	mustCall(t, cs, "task_start", map[string]any{"agent": "main", "title": "Markup", "goal": "a -> b && c <d>"})
	text, isErr := call(t, cs, "task_current", nil)
	if isErr {
		t.Fatalf("task_current returned a tool error: %s", text)
	}
	if !strings.Contains(text, "a -> b && c <d>") {
		t.Errorf("task_current = %s, want the goal as written, with no escapes", text)
	}
}

// TestPanicInAToolIsAnInternalError is step 8 review S2. A panic in a tool handler
// is answered as an internal tool error, with the cause, and the session goes on.
// The SDK does not recover it, as far as its non-test source shows.
func TestPanicInAToolIsAnInternalError(t *testing.T) {
	cs := connect(t, panicking{}, acme)
	text, isErr := call(t, cs, "task_current", nil)
	got := errorOf(t, text, isErr)
	if got.Kind != "internal" || !strings.Contains(got.Message, "nil map write") {
		t.Errorf("panic: %+v, want kind internal with the cause", got)
	}
	text, isErr = call(t, cs, "task_list", map[string]any{})
	errorOf(t, text, isErr)
}

// TestMissingAgentNamesTheArgument is step 8 review S6. The refusal of a write
// with no agent says which argument to give, since on this surface that is the
// one the model sends.
func TestMissingAgentNamesTheArgument(t *testing.T) {
	svc, _ := newService(t)
	cs := connect(t, svc, acme)
	text, isErr := call(t, cs, "learning_add", map[string]any{"kind": "finding", "title": "t", "body": "b"})
	got := errorOf(t, text, isErr)
	if got.Kind != "invalid" || !strings.Contains(got.Message, "needs an agent name") || !strings.Contains(got.Message, "agent argument") {
		t.Errorf("learning_add with no agent: %+v, want invalid naming the agent argument", got)
	}
}

// TestUnresolvedProjectSaysToRestart is step 8 review S9. The project is read once,
// when the session starts, so the refusal says to restart the session after the fix.
// The shared core.ErrNoProject is copied, not changed.
func TestUnresolvedProjectSaysToRestart(t *testing.T) {
	svc, _ := newService(t)
	cs := connect(t, svc, cairnmcp.Project{Err: core.ErrNoProject})
	text, isErr := call(t, cs, "task_current", nil)
	got := errorOf(t, text, isErr)
	if got.Kind != "invalid" || !strings.Contains(got.Message, "cairn init") || !strings.Contains(got.Message, "restart this session") {
		t.Errorf("no project: %+v, want invalid naming cairn init and the restart", got)
	}
	if strings.Contains(core.ErrNoProject.Error(), "restart") {
		t.Errorf("core.ErrNoProject was changed: %q", core.ErrNoProject.Error())
	}
}
