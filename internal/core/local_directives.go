package core

import (
	"context"
	"slices"
	"time"

	"github.com/djm56/cairn-memory/internal/store"
)

// DirectiveCap is the most directives one call returns: task_current, directive_list
// and recall (CONTRACT §5, decision D16). Directives past it are left out and
// counted in omitted.
const DirectiveCap = 12

// DirectiveReviewDays is how many days after ratification a directive is due for
// review, when the ratify call names no review_by (decision D16).
const DirectiveReviewDays = 90

// reviewByLayout is the form of a review_by date in a ratify call: a calendar day
// in UTC, such as 2026-12-31. The stored value is the start of that day.
const reviewByLayout = "2006-01-02"

// directiveFromRow converts a stored directive to the core form. A ratified
// time that is absent is the zero time; every stored directive has one.
func directiveFromRow(r store.DirectiveRow) Directive {
	d := Directive{
		ID: r.ID, Code: r.Code, Scope: DirectiveScope(r.Scope), Project: r.Project,
		Agent: r.Agent, Title: r.Title, AppliesWhen: r.AppliesWhen, Rule: r.Rule,
		Rationale: r.Rationale, ReviewerCheck: r.ReviewerCheck, Example: r.Example,
		Status: DirectiveStatus(r.Status), SourceLearning: r.SourceLearning,
		RatifiedBy: r.RatifiedBy, RetiredAt: r.RetiredAt, ReviewBy: r.ReviewBy,
	}
	if r.RatifiedAt != nil {
		d.RatifiedAt = *r.RatifiedAt
	}
	return d
}

// directiveScopeRank orders the scopes for ranking: agent first, then project,
// then global (CONTRACT §5).
func directiveScopeRank(s DirectiveScope) int {
	switch s {
	case ScopeAgent:
		return 0
	case ScopeProject:
		return 1
	default:
		return 2
	}
}

// rankDirectives orders two directives for the cap: scope rank first, then the
// newer ratification, then the higher id. The id breaks a tie, so the order is
// total and the same on every call.
func rankDirectives(a, b Directive) int {
	if ra, rb := directiveScopeRank(a.Scope), directiveScopeRank(b.Scope); ra != rb {
		return ra - rb
	}
	if !a.RatifiedAt.Equal(b.RatifiedAt) {
		if a.RatifiedAt.After(b.RatifiedAt) {
			return -1
		}
		return 1
	}
	switch {
	case a.ID > b.ID:
		return -1
	case a.ID < b.ID:
		return 1
	}
	return 0
}

// directiveSet is what the three directive reads return: the ranked directives
// within the cap, how many applicable ones are expired, and how many were omitted
// by the cap.
type directiveSet struct {
	Directives []Directive
	Expired    int
	Omitted    int
}

// directivesFor returns the directives that apply to a caller in project, as the
// three directive reads give them. agent narrows the agent-scoped ones to that
// role, and an empty agent includes all of them (CONTRACT §5).
//
// A directive whose review_by is before now is expired: it is left out and
// counted. A retired directive is never listed and is not counted as expired.
// The rest are ranked (rankDirectives) and capped at DirectiveCap; the number
// beyond the cap is omitted.
//
// Read function: call it on the store or inside a transaction.
func directivesFor(ctx context.Context, q store.Querier, project, agent string, now time.Time) (directiveSet, error) {
	rows, err := store.ListActiveDirectives(ctx, q, project, agent)
	if err != nil {
		return directiveSet{}, err
	}
	var set directiveSet
	live := make([]Directive, 0, len(rows))
	for _, r := range rows {
		d := directiveFromRow(r)
		if d.ReviewBy != nil && d.ReviewBy.Before(now) {
			set.Expired++
			continue
		}
		live = append(live, d)
	}
	slices.SortFunc(live, rankDirectives)
	if len(live) > DirectiveCap {
		set.Omitted = len(live) - DirectiveCap
		live = live[:DirectiveCap]
	}
	set.Directives = live
	return set, nil
}

// DirectiveList returns the active directives the caller's project applies:
// every global one, the project's own, and the agent-scoped ones (narrowed to
// req.Agent when it is set). Retired directives are never listed. The list is
// ranked and capped at DirectiveCap; expired and omitted directives are counted.
func (l *Local) DirectiveList(ctx context.Context, req DirectiveListRequest) (*DirectiveList, error) {
	c, err := callerFor(ctx, false)
	if err != nil {
		return nil, err
	}
	if err := l.ensureReadProject(ctx, c); err != nil {
		return nil, err
	}
	set, err := directivesFor(ctx, l.st, c.Project, req.Agent, l.now())
	if err != nil {
		return nil, err
	}
	return &DirectiveList{Directives: set.Directives, Expired: set.Expired, Omitted: set.Omitted}, nil
}
