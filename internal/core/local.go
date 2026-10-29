package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/djm56/cairn-memory/internal/store"
)

// Local is the Service over the local SQLite store (DESIGN.md §5.3). It holds
// the rules: caller and project checks, operator-only decisions, status
// transitions, the one-active rule, the close policy, step positions and the
// journal entries that the calls write.
//
// Every method of Service is implemented here, so Local embeds nothing. The
// compile-time check below covers the whole interface.
type Local struct {
	st *store.Store
}

var _ Service = (*Local)(nil)

// NewLocal returns a Service over st. The store must stay open for the life
// of the Service.
func NewLocal(st *store.Store) *Local {
	return &Local{st: st}
}

// now is the server's time for every record. It is UTC (CONTRACT §1.3).
func (l *Local) now() time.Time {
	return time.Now().UTC()
}

// callerFor returns the caller in ctx for an agent call. Every agent call needs
// a project (CONTRACT §1.1), reads included, and every write also needs an
// agent. A missing project is the error that names `cairn init`.
func callerFor(ctx context.Context, write bool) (Caller, error) {
	c, ok := CallerFrom(ctx)
	if !ok || c.Project == "" {
		return c, ErrNoProject
	}
	if !ValidProjectName(c.Project) {
		return c, Invalid("project name %q is not valid: lowercase letters and digits in hyphen-separated groups, at most 64 characters", c.Project)
	}
	if write && c.Agent == "" {
		return c, Invalid("this call writes, so the caller needs an agent name")
	}
	return c, nil
}

// ensureProject creates the caller's project on its first call and moves
// last_seen_at on (CONTRACT §1.1). It runs inside a write transaction. It
// returns the project's settings, which the rules read.
func ensureProject(ctx context.Context, q store.Querier, project string, now time.Time) (ProjectSettings, error) {
	defaults, err := json.Marshal(DefaultSettings())
	if err != nil {
		return ProjectSettings{}, fmt.Errorf("core: encode default settings: %w", err)
	}
	err = store.EnsureProject(ctx, q, store.ProjectRow{
		Name: project, CreatedAt: now, LastSeenAt: now, Settings: string(defaults),
	})
	if err != nil {
		return ProjectSettings{}, err
	}
	row, err := store.GetProject(ctx, q, project)
	if err != nil {
		return ProjectSettings{}, err
	}
	var s ProjectSettings
	if err := json.Unmarshal([]byte(row.Settings), &s); err != nil {
		return ProjectSettings{}, fmt.Errorf("core: settings of project %s: %w", project, err)
	}
	return s, nil
}

// ensureReadProject makes sure the caller's project exists before a read. A
// read usually finds the row and writes nothing. Only the first call of a new
// project opens a write transaction.
func (l *Local) ensureReadProject(ctx context.Context, project string) error {
	_, err := store.GetProject(ctx, l.st, project)
	if err == nil {
		return nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return l.st.WithTx(ctx, func(q store.Querier) error {
		_, err := ensureProject(ctx, q, project, l.now())
		return err
	})
}

// loadTask returns task id, but only when it belongs to project. A task of
// another project is not_found (CONTRACT §1.2). Every rule that reads a child
// table starts here.
func loadTask(ctx context.Context, q store.Querier, project, id string) (store.TaskRow, error) {
	row, err := store.GetTask(ctx, q, project, id)
	if errors.Is(err, store.ErrNotFound) {
		return row, NotFound("task %s not found", id)
	}
	return row, err
}

// refuseTerminal refuses a write to a task that is done, failed or abandoned.
// After one of them every write to the task is refused (CONTRACT §1.5).
func refuseTerminal(row store.TaskRow) error {
	if s := TaskStatus(row.Status); s.Terminal() {
		return Refused("task %s is %s, and a closed task takes no changes", row.ID, s)
	}
	return nil
}

// touchTask writes the task row with a new updated_at. Every write to a task
// counts as an update, so task_list shows the task where it was last changed.
func touchTask(ctx context.Context, q store.Querier, row *store.TaskRow, now time.Time) error {
	row.UpdatedAt = now
	return store.UpdateTask(ctx, q, *row)
}

// journal appends one event to the task's journal. The caller's agent is the
// actor. step is nil for an event about the whole task.
func journal(ctx context.Context, q store.Querier, task store.TaskRow, agent string,
	step *int64, kind EventKind, text string, at time.Time) error {
	_, err := store.InsertEvent(ctx, q, store.EventRow{
		TaskID: task.ID, Project: task.Project, StepID: step, Agent: agent,
		Kind: string(kind), Text: text, OccurredAt: at,
	})
	return err
}

// blank reports whether s has no text once spaces are trimmed. A required
// text field that is blank is invalid.
func blank(s string) bool {
	return strings.TrimSpace(s) == ""
}

// validPriority reports whether p is one of the four priorities.
func validPriority(p Priority) bool {
	switch p {
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent:
		return true
	}
	return false
}

// validStatus reports whether s is one of the six task statuses.
func validStatus(s TaskStatus) bool {
	switch s {
	case TaskOpen, TaskActive, TaskBlocked, TaskDone, TaskFailed, TaskAbandoned:
		return true
	}
	return false
}

// taskFromRow converts a stored task to the core form. The conversion copies
// fields only; no rule is in it.
func taskFromRow(r store.TaskRow) Task {
	return Task{
		ID: r.ID, Project: r.Project, Title: r.Title, Goal: r.Goal,
		Context: r.Context, Constraints: r.Constraints,
		Status: TaskStatus(r.Status), Priority: Priority(r.Priority),
		Owner: r.Owner, NextAction: r.NextAction, Blockers: r.Blockers,
		OutcomeSummary: r.OutcomeSummary, OpenedAt: r.OpenedAt,
		StartedAt: r.StartedAt, ClosedAt: r.ClosedAt, UpdatedAt: r.UpdatedAt,
	}
}

// taskSummaryFromRow converts a stored task to one line of a task list.
func taskSummaryFromRow(r store.TaskRow) TaskSummary {
	return TaskSummary{
		ID: r.ID, Project: r.Project, Title: r.Title, Status: TaskStatus(r.Status),
		Priority: Priority(r.Priority), UpdatedAt: r.UpdatedAt,
	}
}

// criteriaFromRows converts stored criteria, in ord order.
func criteriaFromRows(rows []store.CriterionRow) []Criterion {
	out := make([]Criterion, 0, len(rows))
	for _, r := range rows {
		out = append(out, Criterion{
			Ord: r.Ord, Criterion: r.Criterion,
			Result: CriterionResult(r.Result), Evidence: r.Evidence,
		})
	}
	return out
}

// filesFromRows converts stored key files or deliverables.
func filesFromRows(rows []store.FileRow) []TaskFile {
	out := make([]TaskFile, 0, len(rows))
	for _, r := range rows {
		out = append(out, TaskFile{Path: r.Path, Role: FileRole(r.Role), Note: r.Note})
	}
	return out
}

// lessonsFromRows converts stored lessons, in ord order.
func lessonsFromRows(rows []store.LessonRow) []Lesson {
	out := make([]Lesson, 0, len(rows))
	for _, r := range rows {
		out = append(out, Lesson{
			Kind: LessonKind(r.Kind), Text: r.Text, Why: r.Why, Correction: r.Correction,
		})
	}
	return out
}

// mistakesFromRows converts stored mistakes, in ord order.
func mistakesFromRows(rows []store.MistakeRow) []Mistake {
	out := make([]Mistake, 0, len(rows))
	for _, r := range rows {
		out = append(out, Mistake{
			Agent: r.Agent, Mistake: r.Mistake, Impact: Impact(r.Impact),
			Correction: r.Correction, Prevention: r.Prevention,
		})
	}
	return out
}

// stepFromRow converts a stored step, with its displayed number.
func stepFromRow(r store.StepRow) Step {
	return Step{
		ID: r.ID, Number: r.Number, TaskID: r.TaskID, Title: r.Title, Agent: r.Agent,
		Status: StepStatus(r.Status), Attempts: r.Attempts, Summary: r.Summary,
		StartedAt: r.StartedAt, EndedAt: r.EndedAt,
	}
}

// stepsFromRows converts stored steps, in display order.
func stepsFromRows(rows []store.StepRow) []Step {
	out := make([]Step, 0, len(rows))
	for _, r := range rows {
		out = append(out, stepFromRow(r))
	}
	return out
}

// reviewFromRow converts a stored review.
func reviewFromRow(r store.ReviewRow) Review {
	return Review{
		ID: r.ID, TaskID: r.TaskID, StepID: r.StepID, Reviewer: r.Reviewer,
		Verdict: Verdict(r.Verdict), Critical: r.Critical, Warnings: r.Warnings,
		Summary: r.Summary, CreatedAt: r.CreatedAt,
	}
}

// eventFromRow converts a stored event. An empty data column is no data.
func eventFromRow(r store.EventRow) Event {
	e := Event{
		ID: r.ID, TaskID: r.TaskID, Project: r.Project, StepID: r.StepID,
		Agent: r.Agent, SessionID: r.SessionID, Kind: EventKind(r.Kind),
		Text: r.Text, OccurredAt: r.OccurredAt,
	}
	if r.Data != "" {
		e.Data = json.RawMessage(r.Data)
	}
	return e
}
