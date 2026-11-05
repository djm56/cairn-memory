package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// fakeWrites is a core.Service whose write calls answer what a test sets, and which records the
// context and the request of each call. A test sets err to have every call refused.
type fakeWrites struct {
	core.Unimplemented
	calls int
	ctx   context.Context
	err   error

	start    *core.TaskStartResult
	gotStart core.TaskStartRequest

	task      *core.Task
	gotUpdate core.TaskUpdateRequest
	gotClose  core.TaskCloseRequest

	list    *core.StepList
	gotPlan core.StepPlanRequest

	step       *core.Step
	gotAdd     core.StepAddRequest
	gotStepUpd core.StepUpdateRequest

	event    *core.Event
	gotEvent core.EventLogRequest
}

func (f *fakeWrites) TaskStart(ctx context.Context, req core.TaskStartRequest) (*core.TaskStartResult, error) {
	f.calls++
	f.ctx, f.gotStart = ctx, req
	return f.start, f.err
}

func (f *fakeWrites) TaskUpdate(ctx context.Context, req core.TaskUpdateRequest) (*core.Task, error) {
	f.calls++
	f.ctx, f.gotUpdate = ctx, req
	return f.task, f.err
}

func (f *fakeWrites) TaskClose(ctx context.Context, req core.TaskCloseRequest) (*core.Task, error) {
	f.calls++
	f.ctx, f.gotClose = ctx, req
	return f.task, f.err
}

func (f *fakeWrites) StepPlan(ctx context.Context, req core.StepPlanRequest) (*core.StepList, error) {
	f.calls++
	f.ctx, f.gotPlan = ctx, req
	return f.list, f.err
}

func (f *fakeWrites) StepAdd(ctx context.Context, req core.StepAddRequest) (*core.Step, error) {
	f.calls++
	f.ctx, f.gotAdd = ctx, req
	return f.step, f.err
}

func (f *fakeWrites) StepUpdate(ctx context.Context, req core.StepUpdateRequest) (*core.Step, error) {
	f.calls++
	f.ctx, f.gotStepUpd = ctx, req
	return f.step, f.err
}

func (f *fakeWrites) EventLog(ctx context.Context, req core.EventLogRequest) (*core.Event, error) {
	f.calls++
	f.ctx, f.gotEvent = ctx, req
	return f.event, f.err
}

// runWrite runs a sub-command with svc behind it and stdin as its standard input, and returns
// its exit code and what it wrote to stdout and stderr. The variable naming the agent is left as
// the test set it, so a test that needs no agent clears it first.
func runWrite(t *testing.T, cmd command, svc core.Service, stdin string, args ...string) (int, string, string) {
	t.Helper()
	open := func() (Session, error) {
		return Session{Svc: svc, Ctx: core.WithCaller(context.Background(), testCaller)}, nil
	}
	var out, errOut bytes.Buffer
	code := cmd(open, args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// agentOf returns the agent the call ran as, as the context of the call carries it.
func agentOf(t *testing.T, ctx context.Context) string {
	t.Helper()
	c, ok := core.CallerFrom(ctx)
	if !ok {
		t.Fatal("the call carried no caller")
	}
	return c.Agent
}
