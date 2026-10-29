package core

import (
	"context"

	"github.com/djm56/cairn-memory/internal/store"
)

// validLearningKind reports whether k is one of the three learning kinds.
func validLearningKind(k LearningKind) bool {
	switch k {
	case LearningFinding, LearningPattern, LearningAgentNote:
		return true
	}
	return false
}

// validLearningStatus reports whether s is one of the six learning statuses.
func validLearningStatus(s LearningStatus) bool {
	switch s {
	case LearningProposed, LearningAccepted, LearningRejected, LearningApplied,
		LearningRetired, LearningSuperseded:
		return true
	}
	return false
}

// learningFromRow converts a stored learning to the core form. The conversion
// copies fields only; no rule is in it.
func learningFromRow(r store.LearningRow) Learning {
	return Learning{
		ID: r.ID, Project: r.Project, TaskID: r.TaskID, Agent: r.Agent,
		Kind: LearningKind(r.Kind), Title: r.Title, Body: r.Body, Target: r.Target,
		Rationale: r.Rationale, Example: r.Example, Status: LearningStatus(r.Status),
		SupersededBy: r.SupersededBy, DecidedBy: r.DecidedBy, DecidedAt: r.DecidedAt,
		DecisionNote: r.DecisionNote, CreatedAt: r.CreatedAt,
	}
}

// LearningAdd records a finding, pattern or agent note. It is always proposed:
// no agent call changes its status (CONTRACT §5). A task_id, when given, must
// name a task of the caller's project. A learning is not refused on a closed
// task, because CONTRACT §1.5 lists the writes a closed task refuses and
// learnings are not among them.
func (l *Local) LearningAdd(ctx context.Context, req LearningAddRequest) (*Learning, error) {
	c, err := callerFor(ctx, true)
	if err != nil {
		return nil, err
	}
	if !validLearningKind(req.Kind) {
		return nil, Invalid("unknown learning kind %q: use finding, pattern or agent-note", req.Kind)
	}
	if blank(req.Title) {
		return nil, Invalid("a learning needs a title")
	}
	if blank(req.Body) {
		return nil, Invalid("a learning needs a body")
	}
	var out Learning
	err = l.st.WithTx(ctx, func(q store.Querier) error {
		now := l.now()
		if _, err := ensureProject(ctx, q, c.Project, now); err != nil {
			return err
		}
		if req.TaskID != "" {
			if _, err := loadTask(ctx, q, c.Project, req.TaskID); err != nil {
				return err
			}
		}
		row := store.LearningRow{
			Project: c.Project, TaskID: req.TaskID, Agent: c.Agent,
			Kind: string(req.Kind), Title: req.Title, Body: req.Body,
			Target: req.Target, Rationale: req.Rationale, Example: req.Example,
			Status: string(LearningProposed), CreatedAt: now,
		}
		id, err := store.InsertLearning(ctx, q, row)
		if err != nil {
			return err
		}
		row.ID = id
		out = learningFromRow(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// LearningList returns learnings, newest first, one page at a time. Only the
// caller's project is listed unless AllProjects is set. Status and kind are
// lists, and an empty list means every value.
func (l *Local) LearningList(ctx context.Context, req LearningListRequest) (*LearningList, error) {
	c, err := callerFor(ctx, false)
	if err != nil {
		return nil, err
	}
	statuses := make([]string, 0, len(req.Status))
	for _, s := range req.Status {
		if !validLearningStatus(s) {
			return nil, Invalid("unknown learning status %q", s)
		}
		statuses = append(statuses, string(s))
	}
	kinds := make([]string, 0, len(req.Kind))
	for _, k := range req.Kind {
		if !validLearningKind(k) {
			return nil, Invalid("unknown learning kind %q: use finding, pattern or agent-note", k)
		}
		kinds = append(kinds, string(k))
	}
	size, err := pageSize(req.Limit, 20)
	if err != nil {
		return nil, err
	}
	before, err := decodeCursor(req.Cursor)
	if err != nil {
		return nil, err
	}
	if err := l.ensureReadProject(ctx, c.Project); err != nil {
		return nil, err
	}
	rows, more, err := store.ListLearnings(ctx, l.st, store.LearningFilter{
		Project: c.Project, AllProjects: req.AllProjects, Statuses: statuses,
		Kinds: kinds, Target: req.Target, Before: before, Limit: size,
	})
	if err != nil {
		return nil, err
	}
	out := &LearningList{Learnings: []Learning{}}
	for _, r := range rows {
		out.Learnings = append(out.Learnings, learningFromRow(r))
	}
	if more && len(rows) > 0 {
		out.Next = encodeCursor(rows[len(rows)-1].ID)
	}
	return out, nil
}
