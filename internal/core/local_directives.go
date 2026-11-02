package core

import (
	"context"

	"github.com/djm56/cairn-memory/internal/store"
)

// directiveFromRow converts a stored directive to the core form. A ratified
// time that is absent is the zero time; every stored directive has one.
func directiveFromRow(r store.DirectiveRow) Directive {
	d := Directive{
		ID: r.ID, Code: r.Code, Scope: DirectiveScope(r.Scope), Project: r.Project,
		Agent: r.Agent, Title: r.Title, AppliesWhen: r.AppliesWhen, Rule: r.Rule,
		Rationale: r.Rationale, ReviewerCheck: r.ReviewerCheck, Example: r.Example,
		Status: DirectiveStatus(r.Status), SourceLearning: r.SourceLearning,
		RatifiedBy: r.RatifiedBy, RetiredAt: r.RetiredAt,
	}
	if r.RatifiedAt != nil {
		d.RatifiedAt = *r.RatifiedAt
	}
	return d
}

// directivesFor returns the active directives that apply to a caller in
// project, ordered by code. agent narrows the agent-scoped ones to that role,
// and an empty agent includes all of them (CONTRACT §5).
//
// Read function: call it on the store or inside a transaction.
func directivesFor(ctx context.Context, q store.Querier, project, agent string) ([]Directive, error) {
	rows, err := store.ListActiveDirectives(ctx, q, project, agent)
	if err != nil {
		return nil, err
	}
	out := make([]Directive, 0, len(rows))
	for _, r := range rows {
		out = append(out, directiveFromRow(r))
	}
	return out, nil
}

// DirectiveList returns the active directives the caller's project applies:
// every global one, the project's own, and the agent-scoped ones (narrowed to
// req.Agent when it is set). Retired directives are never listed.
func (l *Local) DirectiveList(ctx context.Context, req DirectiveListRequest) (*DirectiveList, error) {
	c, err := callerFor(ctx, false)
	if err != nil {
		return nil, err
	}
	if err := l.ensureReadProject(ctx, c); err != nil {
		return nil, err
	}
	ds, err := directivesFor(ctx, l.st, c.Project, req.Agent)
	if err != nil {
		return nil, err
	}
	return &DirectiveList{Directives: ds}, nil
}
