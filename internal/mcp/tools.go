package mcp

import (
	"context"
	"encoding/json"

	"github.com/djm56/cairn-memory/internal/core"
)

// register adds the fifteen tools of DESIGN.md §4 to s, each one call of svc. The
// eight write tools take the caller's agent as "agent" (CONTRACT §1.1). The step
// tools name the step's assignee "assignee", since "agent" is the caller there.
func register(s *Server, svc core.Service) {
	addTool(s, "task_current",
		"The active task, with its steps, the last events and the directives that apply. Call it first in a session.", false,
		func(ctx context.Context, _ noInput) (any, error) { return svc.TaskCurrent(ctx) })

	addTool(s, "task_start",
		"Start a task with its brief and an optional plan. Returns its id. Refused while another task is active, unless the project allows parallel tasks.", true,
		func(ctx context.Context, in taskStartIn) (any, error) {
			return svc.TaskStart(ctx, core.TaskStartRequest{
				Title: in.Title, Goal: in.Goal, Context: in.Context, Constraints: in.Constraints,
				Priority: in.Priority, Criteria: in.Criteria, KeyFiles: in.KeyFiles, Steps: in.Steps, Queue: in.Queue,
			})
		})

	addTool(s, "task_get",
		"One task in full, or one section: brief, steps, events or close. events is the paged journal; limit, cursor and step filter it.", false,
		func(ctx context.Context, in taskGetIn) (any, error) {
			return svc.TaskGet(ctx, core.TaskGetRequest{
				ID: in.ID, Section: in.Section, Limit: in.Limit, Cursor: in.Cursor, Step: in.Step,
			})
		})

	addTool(s, "task_list",
		"Recent tasks in this project, most recently updated first. Filter by status; paged.", false,
		func(ctx context.Context, in taskListIn) (any, error) {
			return svc.TaskList(ctx, core.TaskListRequest{Status: in.Status, Limit: in.Limit, Cursor: in.Cursor})
		})

	addTool(s, "task_update",
		"Change a task's status, priority, owner, next action or blockers, or edit its brief before any step starts. Only the fields given change.", true,
		func(ctx context.Context, in taskUpdateIn) (any, error) {
			return svc.TaskUpdate(ctx, core.TaskUpdateRequest{
				ID: in.ID, Status: in.Status, Reason: in.Reason, Priority: in.Priority, Owner: in.Owner,
				NextAction: in.NextAction, Blockers: in.Blockers, Title: in.Title, Goal: in.Goal,
				Context: in.Context, Constraints: in.Constraints, Criteria: in.Criteria, KeyFiles: in.KeyFiles,
			})
		})

	addTool(s, "task_close",
		"Close a task with its outcome, summary, criteria results, lessons and mistakes. Refused with the list of what is missing.", true,
		func(ctx context.Context, in taskCloseIn) (any, error) {
			return svc.TaskClose(ctx, core.TaskCloseRequest{
				ID: in.ID, Outcome: in.Outcome, Summary: in.Summary, Criteria: in.Criteria, Lessons: in.Lessons,
				NoLessonsReason: in.NoLessonsReason, Mistakes: in.Mistakes, Deliverables: in.Deliverables,
			})
		})

	addTool(s, "step_plan",
		"Replace the task's pending steps with this list. Started steps are kept. A pending step that has an event cannot be removed: skip it first.", true,
		func(ctx context.Context, in stepPlanIn) (any, error) {
			return svc.StepPlan(ctx, core.StepPlanRequest{TaskID: in.TaskID, Steps: in.Steps})
		})

	addTool(s, "step_add",
		"Insert one step after a given step, or at the end of the plan. after number 0 puts it first.", true,
		func(ctx context.Context, in stepAddIn) (any, error) {
			return svc.StepAdd(ctx, core.StepAddRequest{TaskID: in.TaskID, After: in.After, Title: in.Title, Agent: in.Assignee})
		})

	addTool(s, "step_update",
		"Start, finish, fail or skip a step. finish, fail and skip need a summary. assignee reassigns the step.", true,
		func(ctx context.Context, in stepUpdateIn) (any, error) {
			return svc.StepUpdate(ctx, core.StepUpdateRequest{
				TaskID: in.TaskID, Step: in.Step, Action: in.Action, Summary: in.Summary, Agent: in.Assignee,
			})
		})

	addTool(s, "event_log",
		"Append a journal event: note, blocked, unblocked, handoff, decision, review or custom. A review needs data with verdict, critical and warnings.", true,
		func(ctx context.Context, in eventLogIn) (any, error) {
			return svc.EventLog(ctx, core.EventLogRequest{
				TaskID: in.TaskID, Step: in.Step, Kind: in.Kind, Text: in.Text, Data: in.Data,
			})
		})

	addTool(s, "learning_add",
		"Propose a finding, pattern or agent note. It is stored as proposed; the operator decides what happens to it.", true,
		func(ctx context.Context, in learningAddIn) (any, error) {
			return svc.LearningAdd(ctx, core.LearningAddRequest{
				TaskID: in.TaskID, Kind: in.Kind, Title: in.Title, Body: in.Body,
				Target: in.Target, Rationale: in.Rationale, Example: in.Example,
			})
		})

	addTool(s, "learning_list",
		"Learnings in this project by status, kind or target, newest first. Set all_projects to see every project. Paged.", false,
		func(ctx context.Context, in learningListIn) (any, error) {
			return svc.LearningList(ctx, core.LearningListRequest{
				Status: in.Status, Kind: in.Kind, AllProjects: in.AllProjects, Target: in.Target,
				Limit: in.Limit, Cursor: in.Cursor,
			})
		})

	addTool(s, "directive_list",
		"The active directives that apply to this project. Pass agent to narrow the agent-scoped ones to one role.", false,
		func(ctx context.Context, in directiveListIn) (any, error) {
			return svc.DirectiveList(ctx, core.DirectiveListRequest{Agent: in.Agent})
		})

	addTool(s, "recall",
		"Before a task: the directives, lessons, mistakes and similar tasks for a goal, from every project. Pass agent to narrow directives to one role.", false,
		func(ctx context.Context, in recallIn) (any, error) {
			return svc.Recall(ctx, core.RecallRequest{Goal: in.Goal, Agent: in.Agent, Limit: in.Limit})
		})

	addTool(s, "search",
		"Full-text search over tasks, events, learnings, directives, lessons and mistakes. Plain words only. Paged.", false,
		func(ctx context.Context, in searchIn) (any, error) {
			return svc.Search(ctx, core.SearchRequest{
				Query: in.Query, Kinds: in.Kinds, AllProjects: in.AllProjects, Limit: in.Limit, Cursor: in.Cursor,
			})
		})
}

// The argument types of the tools. Each field's JSON name is the one CONTRACT
// names; the jsonschema tag is what a model reads as the field's description.
// A field that is absent is nil, or empty, and core reads it the same way it
// reads an absent field of the same request type.

type taskStartIn struct {
	Agent       string           `json:"agent" jsonschema:"the calling agent's name, such as main or hicks"`
	Title       string           `json:"title" jsonschema:"short name of the task"`
	Goal        string           `json:"goal" jsonschema:"one or two sentences: what done looks like"`
	Context     string           `json:"context,omitempty" jsonschema:"why now, background and links; a long brief goes here"`
	Constraints string           `json:"constraints,omitempty" jsonschema:"what must not change"`
	Priority    core.Priority    `json:"priority,omitempty" jsonschema:"low, normal, high or urgent; normal by default"`
	Criteria    []string         `json:"criteria,omitempty" jsonschema:"acceptance criteria, in order"`
	KeyFiles    []core.TaskFile  `json:"key_files,omitempty" jsonschema:"files the brief names: path and note"`
	Steps       []core.StepInput `json:"steps,omitempty" jsonschema:"the plan: each step has a title and an optional agent"`
	Queue       bool             `json:"queue,omitempty" jsonschema:"true creates the task open instead of active"`
}

type taskGetIn struct {
	ID      string           `json:"id" jsonschema:"the task id, such as task-20261030-01"`
	Section core.TaskSection `json:"section,omitempty" jsonschema:"brief, steps, events or close; empty for all of them"`
	Limit   int              `json:"limit,omitempty" jsonschema:"events only: page size, 20 by default, 100 at most"`
	Cursor  string           `json:"cursor,omitempty" jsonschema:"events only: the next value from the last page"`
	Step    *core.StepRef    `json:"step,omitempty" jsonschema:"events only: one step's events, as {\"number\": n} or {\"id\": n}"`
}

type taskListIn struct {
	Status []core.TaskStatus `json:"status,omitempty" jsonschema:"statuses to list; empty lists all"`
	Limit  int               `json:"limit,omitempty" jsonschema:"page size, 20 by default, 100 at most"`
	Cursor string            `json:"cursor,omitempty" jsonschema:"the next value from the last page"`
}

type taskUpdateIn struct {
	Agent       string           `json:"agent" jsonschema:"the calling agent's name, such as main or hicks"`
	ID          string           `json:"id" jsonschema:"the task id"`
	Status      *core.TaskStatus `json:"status,omitempty" jsonschema:"open is not allowed here; active, blocked or abandoned"`
	Reason      string           `json:"reason,omitempty" jsonschema:"required for blocked and abandoned"`
	Priority    *core.Priority   `json:"priority,omitempty" jsonschema:"low, normal, high or urgent"`
	Owner       *string          `json:"owner,omitempty" jsonschema:"the agent leading the task"`
	NextAction  *string          `json:"next_action,omitempty" jsonschema:"the next thing to do"`
	Blockers    *string          `json:"blockers,omitempty" jsonschema:"what is blocking the task"`
	Title       *string          `json:"title,omitempty" jsonschema:"brief: only before any step starts"`
	Goal        *string          `json:"goal,omitempty" jsonschema:"brief: only before any step starts"`
	Context     *string          `json:"context,omitempty" jsonschema:"brief: only before any step starts"`
	Constraints *string          `json:"constraints,omitempty" jsonschema:"brief: only before any step starts"`
	Criteria    *[]string        `json:"criteria,omitempty" jsonschema:"brief: replaces the whole list"`
	KeyFiles    *[]core.TaskFile `json:"key_files,omitempty" jsonschema:"brief: replaces the whole list"`
}

type taskCloseIn struct {
	Agent           string                  `json:"agent" jsonschema:"the calling agent's name, such as main or hicks"`
	ID              string                  `json:"id" jsonschema:"the task id"`
	Outcome         core.TaskStatus         `json:"outcome,omitempty" jsonschema:"done or failed"`
	Summary         string                  `json:"summary,omitempty" jsonschema:"what was done, in a few sentences"`
	Criteria        []core.CriterionOutcome `json:"criteria,omitempty" jsonschema:"one result per acceptance criterion: ord, result met, partial, unmet or unclear, evidence"`
	Lessons         []core.Lesson           `json:"lessons,omitempty" jsonschema:"kind wrong-assumption, reuse or similar-task, with text, why and correction; or none with no_lessons_reason"`
	NoLessonsReason string                  `json:"no_lessons_reason,omitempty" jsonschema:"why there are no lessons"`
	Mistakes        []core.Mistake          `json:"mistakes,omitempty" jsonschema:"agent, mistake, impact low, med or high, correction and prevention; send [] for none"`
	Deliverables    []core.TaskFile         `json:"deliverables,omitempty" jsonschema:"files the task produced: path and note"`
}

type stepPlanIn struct {
	Agent  string           `json:"agent" jsonschema:"the calling agent's name, such as main or hicks"`
	TaskID string           `json:"task_id" jsonschema:"the task id"`
	Steps  []core.StepInput `json:"steps" jsonschema:"the new pending steps: a title and an optional agent each"`
}

type stepAddIn struct {
	Agent    string        `json:"agent" jsonschema:"the calling agent's name, such as main or hicks"`
	TaskID   string        `json:"task_id" jsonschema:"the task id"`
	After    *core.StepRef `json:"after,omitempty" jsonschema:"the step to insert after, as {\"number\": n} or {\"id\": n}; number 0 means first; absent means last"`
	Title    string        `json:"title" jsonschema:"what the step does"`
	Assignee string        `json:"assignee,omitempty" jsonschema:"the agent the step is for"`
}

type stepUpdateIn struct {
	Agent    string          `json:"agent" jsonschema:"the calling agent's name, such as main or hicks"`
	TaskID   string          `json:"task_id" jsonschema:"the task id"`
	Step     core.StepRef    `json:"step" jsonschema:"the step, as {\"number\": n} or {\"id\": n}"`
	Action   core.StepAction `json:"action" jsonschema:"start, finish, fail or skip"`
	Summary  string          `json:"summary,omitempty" jsonschema:"the result; needed for finish, fail and skip"`
	Assignee *string         `json:"assignee,omitempty" jsonschema:"reassign the step to this agent"`
}

type eventLogIn struct {
	Agent  string          `json:"agent" jsonschema:"the calling agent's name, such as main or hicks"`
	TaskID string          `json:"task_id" jsonschema:"the task id"`
	Step   *core.StepRef   `json:"step,omitempty" jsonschema:"the step the event is about, as {\"number\": n} or {\"id\": n}"`
	Kind   core.EventKind  `json:"kind" jsonschema:"note, blocked, unblocked, handoff, decision, review or custom"`
	Text   string          `json:"text" jsonschema:"what happened"`
	Data   json.RawMessage `json:"data,omitempty" jsonschema:"a review needs {\"verdict\": \"approved\" or \"not-approved\", \"critical\": n, \"warnings\": n}"`
}

type learningAddIn struct {
	Agent     string            `json:"agent" jsonschema:"the calling agent's name, such as main or hicks"`
	TaskID    string            `json:"task_id,omitempty" jsonschema:"the task the learning came from"`
	Kind      core.LearningKind `json:"kind" jsonschema:"finding, pattern or agent-note"`
	Title     string            `json:"title" jsonschema:"short title"`
	Body      string            `json:"body" jsonschema:"the suggestion, the solution or the note"`
	Target    string            `json:"target,omitempty" jsonschema:"what it would change: a file, a rule or an agent"`
	Rationale string            `json:"rationale,omitempty" jsonschema:"why"`
	Example   string            `json:"example,omitempty" jsonschema:"an example"`
}

type learningListIn struct {
	Status      []core.LearningStatus `json:"status,omitempty" jsonschema:"statuses to list; empty lists all"`
	Kind        []core.LearningKind   `json:"kind,omitempty" jsonschema:"kinds to list; empty lists all"`
	AllProjects bool                  `json:"all_projects,omitempty" jsonschema:"true lists every project's learnings"`
	Target      string                `json:"target,omitempty" jsonschema:"only learnings with this target"`
	Limit       int                   `json:"limit,omitempty" jsonschema:"page size, 20 by default, 100 at most"`
	Cursor      string                `json:"cursor,omitempty" jsonschema:"the next value from the last page"`
}

type directiveListIn struct {
	Agent string `json:"agent,omitempty" jsonschema:"a role name, such as reviewer; narrows the agent-scoped directives"`
}

type recallIn struct {
	Goal  string `json:"goal" jsonschema:"what the task is for, in a sentence"`
	Agent string `json:"agent,omitempty" jsonschema:"a role name; narrows the directives as directive_list does"`
	Limit int    `json:"limit,omitempty" jsonschema:"items per list, 5 by default"`
}

type searchIn struct {
	Query       string         `json:"query" jsonschema:"plain words; quotes and operators are matched as words"`
	Kinds       []core.HitKind `json:"kinds,omitempty" jsonschema:"task, event, learning, directive, note, lesson or mistake; empty means all"`
	AllProjects bool           `json:"all_projects,omitempty" jsonschema:"true searches every project"`
	Limit       int            `json:"limit,omitempty" jsonschema:"hits per page, 10 by default, 100 at most"`
	Cursor      string         `json:"cursor,omitempty" jsonschema:"the next value from the last page"`
}
