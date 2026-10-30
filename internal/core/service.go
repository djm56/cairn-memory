package core

import (
	"context"
	"errors"
)

// Service is the contract every backend satisfies. A behaviour exists when
// the conformance suite (internal/conformance) says so.
type Service interface {
	Agent
	Operator
}

// Agent is the MCP tool surface (DESIGN.md §4), one method per tool. Every
// call needs a Caller with a project; every write also needs an agent.
type Agent interface {
	TaskCurrent(ctx context.Context) (*TaskCurrent, error)
	TaskStart(ctx context.Context, req TaskStartRequest) (*TaskStartResult, error)
	TaskGet(ctx context.Context, req TaskGetRequest) (*TaskDetail, error)
	TaskList(ctx context.Context, req TaskListRequest) (*TaskList, error)
	TaskUpdate(ctx context.Context, req TaskUpdateRequest) (*Task, error)
	TaskClose(ctx context.Context, req TaskCloseRequest) (*Task, error)

	StepPlan(ctx context.Context, req StepPlanRequest) (*StepList, error)
	StepAdd(ctx context.Context, req StepAddRequest) (*Step, error)
	StepUpdate(ctx context.Context, req StepUpdateRequest) (*Step, error)

	EventLog(ctx context.Context, req EventLogRequest) (*Event, error)

	LearningAdd(ctx context.Context, req LearningAddRequest) (*Learning, error)
	LearningList(ctx context.Context, req LearningListRequest) (*LearningList, error)
	DirectiveList(ctx context.Context, req DirectiveListRequest) (*DirectiveList, error)

	Recall(ctx context.Context, req RecallRequest) (*RecallResult, error)
	Search(ctx context.Context, req SearchRequest) (*SearchResult, error)
}

// Operator holds the decisions only an operator makes (DESIGN.md §3.4,
// §3.7). Each refuses a caller without RoleOperator with KindOperatorOnly.
// None is ever exposed as an MCP tool.
type Operator interface {
	LearningDecide(ctx context.Context, req LearningDecideRequest) (*Learning, error)
	DirectiveRatify(ctx context.Context, req DirectiveRatifyRequest) (*Directive, error)
	DirectiveRetire(ctx context.Context, req DirectiveRetireRequest) (*Directive, error)
	ProjectSettingsSet(ctx context.Context, req ProjectSettingsRequest) (*Project, error)
}

// ErrNotImplemented is returned by every Unimplemented call.
var ErrNotImplemented = errors.New("not implemented")

// Unimplemented is a Service that fails every call with ErrNotImplemented. It is
// for the conformance harness only: the suite runs against it to show that every
// case can fail. No production path returns ErrNotImplemented.
type Unimplemented struct{}

var _ Service = Unimplemented{}

func (Unimplemented) TaskCurrent(context.Context) (*TaskCurrent, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) TaskStart(context.Context, TaskStartRequest) (*TaskStartResult, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) TaskGet(context.Context, TaskGetRequest) (*TaskDetail, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) TaskList(context.Context, TaskListRequest) (*TaskList, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) TaskUpdate(context.Context, TaskUpdateRequest) (*Task, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) TaskClose(context.Context, TaskCloseRequest) (*Task, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) StepPlan(context.Context, StepPlanRequest) (*StepList, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) StepAdd(context.Context, StepAddRequest) (*Step, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) StepUpdate(context.Context, StepUpdateRequest) (*Step, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) EventLog(context.Context, EventLogRequest) (*Event, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) LearningAdd(context.Context, LearningAddRequest) (*Learning, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) LearningList(context.Context, LearningListRequest) (*LearningList, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) DirectiveList(context.Context, DirectiveListRequest) (*DirectiveList, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) Recall(context.Context, RecallRequest) (*RecallResult, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) Search(context.Context, SearchRequest) (*SearchResult, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) LearningDecide(context.Context, LearningDecideRequest) (*Learning, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) DirectiveRatify(context.Context, DirectiveRatifyRequest) (*Directive, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) DirectiveRetire(context.Context, DirectiveRetireRequest) (*Directive, error) {
	return nil, ErrNotImplemented
}
func (Unimplemented) ProjectSettingsSet(context.Context, ProjectSettingsRequest) (*Project, error) {
	return nil, ErrNotImplemented
}
