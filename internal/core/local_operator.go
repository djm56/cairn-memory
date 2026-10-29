package core

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/djm56/cairn-memory/internal/store"
)

// operatorFor returns the caller of an operator call (CONTRACT §8). The role is
// checked first, before anything else in the call: a caller without the
// operator role is operator_only, whatever else is wrong with the request. The
// operator's name is then required on every operator call (CONTRACT §8). Two
// calls record the name on the row they change, decided_by and ratified_by. The
// other two, DirectiveRetire and ProjectSettingsSet, store no name, so for them
// the rule is uniform rather than recorded. Operator calls need no project: they
// act across projects.
func operatorFor(ctx context.Context) (Caller, error) {
	c, ok := CallerFrom(ctx)
	if !ok || c.Role != RoleOperator {
		return c, OperatorOnly("this call is an operator decision, and only an operator may make it")
	}
	if blank(c.Agent) {
		return c, Invalid("an operator call needs the operator's name")
	}
	return c, nil
}

// ensureOperatorProject makes sure project has a row before an operator call
// writes to it. A directive of project scope and a project's settings both
// name the project, and the database refuses a name with no row. An existing
// row is left alone: unlike EnsureProject, this does not move last_seen_at,
// which records agent calls only.
//
// Write function: call it through WithTx.
func ensureOperatorProject(ctx context.Context, q store.Querier, project string, now time.Time) error {
	_, err := store.GetProject(ctx, q, project)
	if err == nil {
		return nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	_, err = ensureProject(ctx, q, project, now)
	return err
}

// LearningDecide records the operator's decision on a learning: its status,
// who decided and when, and the note (CONTRACT §8). A superseded learning names
// the learning that replaces it, and only that status takes one. The links may
// not close a cycle: a learning cannot be superseded, directly or through a
// chain, by a learning that is superseded by it (refused).
func (l *Local) LearningDecide(ctx context.Context, req LearningDecideRequest) (*Learning, error) {
	c, err := operatorFor(ctx)
	if err != nil {
		return nil, err
	}
	switch {
	case req.Status == LearningProposed:
		return nil, Invalid("a decision cannot return a learning to proposed")
	case !validLearningStatus(req.Status):
		return nil, Invalid("unknown learning status %q", req.Status)
	case req.Status == LearningSuperseded && req.SupersededBy == nil:
		return nil, Invalid("a superseded learning needs superseded_by: the learning that replaces it")
	case req.Status != LearningSuperseded && req.SupersededBy != nil:
		return nil, Invalid("superseded_by is set only when the status is superseded")
	case req.SupersededBy != nil && *req.SupersededBy == req.ID:
		return nil, Invalid("a learning cannot be superseded by itself")
	}

	var out Learning
	err = l.st.WithTx(ctx, func(q store.Querier) error {
		row, err := store.GetLearningAnyProject(ctx, q, req.ID)
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("learning %d not found", req.ID)
		}
		if err != nil {
			return err
		}
		if req.SupersededBy != nil {
			if _, err := store.GetLearningAnyProject(ctx, q, *req.SupersededBy); errors.Is(err, store.ErrNotFound) {
				return NotFound("superseded_by names learning %d, which does not exist", *req.SupersededBy)
			} else if err != nil {
				return err
			}
			cycle, err := supersedeReaches(ctx, q, *req.SupersededBy, req.ID)
			if err != nil {
				return err
			}
			if cycle {
				return Refused("learning %d cannot be superseded by %d: %d is superseded by %d, directly or through a chain, so the links would form a cycle",
					req.ID, *req.SupersededBy, *req.SupersededBy, req.ID)
			}
		}
		now := l.now()
		row.Status = string(req.Status)
		row.SupersededBy = req.SupersededBy
		row.DecidedBy = c.Agent
		row.DecidedAt = &now
		row.DecisionNote = req.Note
		if err := store.UpdateLearning(ctx, q, row); err != nil {
			return err
		}
		out = learningFromRow(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// supersedeReaches reports whether learning from is superseded by learning to,
// directly or through a chain of superseded_by links. LearningDecide calls it
// with the learning named as the replacement as from, and the learning being
// decided as to. If the replacement already leads back to the decided learning,
// linking the decided learning to it would close a cycle. The walk stops at a
// learning with no link, and it keeps a set of the learnings it has seen, so a
// cycle already in the data cannot loop it.
//
// Read function: call it inside the transaction that writes the link.
func supersedeReaches(ctx context.Context, q store.Querier, from, to int64) (bool, error) {
	seen := map[int64]bool{}
	cur := from
	for !seen[cur] {
		seen[cur] = true
		row, err := store.GetLearningAnyProject(ctx, q, cur)
		if err != nil {
			return false, err
		}
		if row.SupersededBy == nil {
			return false, nil
		}
		if *row.SupersededBy == to {
			return true, nil
		}
		cur = *row.SupersededBy
	}
	return false, nil
}

// DirectiveRatify creates an active directive with the next code (CONTRACT §8).
// Its scope names what it applies to: a global directive has no project or
// agent, a project directive names a valid project, and an agent directive names
// a role. With FromLearning, the learning must be accepted, and it becomes
// applied in the same transaction.
func (l *Local) DirectiveRatify(ctx context.Context, req DirectiveRatifyRequest) (*Directive, error) {
	c, err := operatorFor(ctx)
	if err != nil {
		return nil, err
	}
	switch req.Scope {
	case ScopeGlobal:
		if req.Project != "" || req.Agent != "" {
			return nil, Invalid("a global directive applies to every project and agent: give no project or agent")
		}
	case ScopeProject:
		if !ValidProjectName(req.Project) {
			return nil, Invalid("a project directive needs a valid project name: %q is not one", req.Project)
		}
		if req.Agent != "" {
			return nil, Invalid("a project directive is not scoped to an agent: give no agent")
		}
	case ScopeAgent:
		if blank(req.Agent) {
			return nil, Invalid("an agent directive needs the role name it applies to")
		}
		if req.Project != "" {
			return nil, Invalid("an agent directive applies in every project: give no project")
		}
	default:
		return nil, Invalid("unknown scope %q: use global, project or agent", req.Scope)
	}
	if blank(req.Title) {
		return nil, Invalid("a directive needs a title")
	}
	if blank(req.Rule) {
		return nil, Invalid("a directive needs a rule")
	}

	var out Directive
	err = l.st.WithTx(ctx, func(q store.Querier) error {
		now := l.now()
		if req.Scope == ScopeProject {
			if err := ensureOperatorProject(ctx, q, req.Project, now); err != nil {
				return err
			}
		}
		if req.FromLearning != nil {
			lrn, err := store.GetLearningAnyProject(ctx, q, *req.FromLearning)
			if errors.Is(err, store.ErrNotFound) {
				return NotFound("learning %d not found", *req.FromLearning)
			}
			if err != nil {
				return err
			}
			if lrn.Status != string(LearningAccepted) {
				return Refused("learning %d is %s: only an accepted learning becomes a directive", lrn.ID, lrn.Status)
			}
			lrn.Status = string(LearningApplied)
			if err := store.UpdateLearning(ctx, q, lrn); err != nil {
				return err
			}
		}
		code, err := store.NextDirectiveCode(ctx, q)
		if err != nil {
			return err
		}
		row := store.DirectiveRow{
			Code: code, Scope: string(req.Scope), Title: req.Title,
			AppliesWhen: req.AppliesWhen, Rule: req.Rule, Rationale: req.Rationale,
			ReviewerCheck: req.ReviewerCheck, Example: req.Example,
			Status: string(DirectiveActive), RatifiedBy: c.Agent, RatifiedAt: &now,
		}
		switch req.Scope {
		case ScopeProject:
			row.Project = req.Project
		case ScopeAgent:
			row.Agent = req.Agent
		}
		if req.FromLearning != nil {
			id := *req.FromLearning
			row.SourceLearning = &id
		}
		id, err := store.InsertDirective(ctx, q, row)
		if err != nil {
			return err
		}
		row.ID = id
		out = directiveFromRow(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DirectiveRetire retires a directive by its code (CONTRACT §8). A retired
// directive is no longer listed, recalled or found by search. A directive that
// is already retired is refused: the retirement happened once, and its time is
// kept.
func (l *Local) DirectiveRetire(ctx context.Context, req DirectiveRetireRequest) (*Directive, error) {
	if _, err := operatorFor(ctx); err != nil {
		return nil, err
	}
	if blank(req.Code) {
		return nil, Invalid("a directive code is required, such as D-0001")
	}

	var out Directive
	err := l.st.WithTx(ctx, func(q store.Querier) error {
		row, err := store.GetDirectiveByCode(ctx, q, req.Code)
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("directive %s not found", req.Code)
		}
		if err != nil {
			return err
		}
		if row.Status == string(DirectiveRetired) {
			return Refused("directive %s is already retired", row.Code)
		}
		now := l.now()
		if err := store.UpdateDirectiveStatus(ctx, q, row.Code, string(DirectiveRetired), &now); err != nil {
			return err
		}
		row.Status = string(DirectiveRetired)
		row.RetiredAt = &now
		out = directiveFromRow(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ProjectSettingsSet replaces a project's settings: parallel tasks and the
// close policy (CONTRACT §8). The project is created if it has no row yet. A nil
// display name leaves the stored one as it is. Settings must be given: an omitted
// object is invalid, since the call replaces the whole of it.
func (l *Local) ProjectSettingsSet(ctx context.Context, req ProjectSettingsRequest) (*Project, error) {
	if _, err := operatorFor(ctx); err != nil {
		return nil, err
	}
	if !ValidProjectName(req.Project) {
		return nil, Invalid("a project needs a valid name: %q is not one", req.Project)
	}
	if req.Settings == nil {
		return nil, Invalid("project_settings_set needs settings: the call replaces the whole object, so give all of it")
	}
	raw, err := json.Marshal(*req.Settings)
	if err != nil {
		return nil, err
	}

	var out Project
	err = l.st.WithTx(ctx, func(q store.Querier) error {
		now := l.now()
		if err := ensureOperatorProject(ctx, q, req.Project, now); err != nil {
			return err
		}
		row, err := store.GetProject(ctx, q, req.Project)
		if err != nil {
			return err
		}
		row.Settings = string(raw)
		if req.DisplayName != nil {
			row.DisplayName = *req.DisplayName
		}
		if err := store.UpdateProject(ctx, q, row); err != nil {
			return err
		}
		out = Project{
			Name: row.Name, DisplayName: row.DisplayName, Settings: *req.Settings,
			CreatedAt: row.CreatedAt, LastSeenAt: row.LastSeenAt,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
