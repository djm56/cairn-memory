package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/djm56/cairn-memory/internal/store"
)

// dashboardRunningLimit is how many running tasks the dashboard lists (CONTRACT §8.1).
const dashboardRunningLimit = 20

// dashboardWindowDays is the length of the quality window, in whole UTC days
// (CONTRACT §8.1, quality_30d).
const dashboardWindowDays = 30

// ReaderOnlyMessage is the message of an agent's refused web UI read. The wire form
// (internal/api) gives this same text, so the two answers read the same. One constant
// holds it, so the two cannot drift apart (step 6 review S3).
const ReaderOnlyMessage = "this read is for the web UI: only an operator or a viewer key may make it"

// readerFor returns the caller of a web UI read (CONTRACT §8.1, §9 item 50). The role is
// checked first, before anything else in the call. Only the operator and viewer roles
// may read: an agent, or a caller with no role, is operator_only. The read needs no
// project and no agent name.
func readerFor(ctx context.Context) (Caller, error) {
	c, ok := CallerFrom(ctx)
	if !ok || !isReader(c.Role) {
		return c, OperatorOnly(ReaderOnlyMessage)
	}
	return c, nil
}

// isReader reports whether role may make the web UI reads: the operator and the viewer.
func isReader(role Role) bool {
	return role == RoleOperator || role == RoleViewer
}

// projectFromRow converts a stored project to the core form. Its settings are read by
// decodeSettings, the reader the close rules use too, so a '{}' value reads as
// DefaultSettings here and in enforcement (CONTRACT §8.1, project_list).
func projectFromRow(r store.ProjectRow) (Project, error) {
	settings, err := decodeSettings(r.Settings)
	if err != nil {
		return Project{}, fmt.Errorf("core: settings of project %s: %w", r.Name, err)
	}
	return Project{
		Name: r.Name, DisplayName: r.DisplayName, Settings: settings,
		CreatedAt: r.CreatedAt, LastSeenAt: r.LastSeenAt,
	}, nil
}

// ProjectList returns the projects, ordered by name, one page at a time, or the one
// project that req.Project names (CONTRACT §8.1). It creates no project row.
func (l *Local) ProjectList(ctx context.Context, req ProjectListRequest) (*ProjectList, error) {
	if _, err := readerFor(ctx); err != nil {
		return nil, err
	}
	if req.Project != "" && !ValidProjectName(req.Project) {
		return nil, Invalid("project name %q is not valid: lowercase letters and digits in hyphen-separated groups, at most 64 characters", req.Project)
	}
	size, err := pageSize(req.Limit, 20)
	if err != nil {
		return nil, err
	}
	after, err := decodeNameCursor(req.Cursor)
	if err != nil {
		return nil, err
	}
	out := &ProjectList{Projects: []Project{}}
	err = l.st.WithReadTx(ctx, func(q store.Querier) error {
		if req.Project != "" {
			row, err := store.GetProject(ctx, q, req.Project)
			if errors.Is(err, store.ErrNotFound) {
				return NotFound("project %s not found", req.Project)
			}
			if err != nil {
				return err
			}
			p, err := projectFromRow(row)
			if err != nil {
				return err
			}
			out.Projects = append(out.Projects, p)
			return nil
		}
		rows, more, err := store.ListProjects(ctx, q, after, size)
		if err != nil {
			return err
		}
		for _, r := range rows {
			p, err := projectFromRow(r)
			if err != nil {
				return err
			}
			out.Projects = append(out.Projects, p)
		}
		if more && len(rows) > 0 {
			out.Next = encodeNameCursor(rows[len(rows)-1].Name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// TaskBrowse returns the tasks of every project, or of req.Project, most recently updated
// first, with id descending breaking a tie (CONTRACT §8.1). A named project with no row is
// not_found, and no row is created.
func (l *Local) TaskBrowse(ctx context.Context, req TaskBrowseRequest) (*TaskBrowse, error) {
	if _, err := readerFor(ctx); err != nil {
		return nil, err
	}
	if req.Project != "" && !ValidProjectName(req.Project) {
		return nil, Invalid("project name %q is not valid: lowercase letters and digits in hyphen-separated groups, at most 64 characters", req.Project)
	}
	statuses := make([]string, 0, len(req.Status))
	for _, s := range req.Status {
		if !validStatus(s) {
			return nil, Invalid("unknown task status %q", s)
		}
		statuses = append(statuses, string(s))
	}
	size, err := pageSize(req.Limit, 20)
	if err != nil {
		return nil, err
	}
	after, err := decodeTaskCursor(req.Cursor)
	if err != nil {
		return nil, err
	}
	out := &TaskBrowse{Tasks: []TaskSummary{}}
	err = l.st.WithReadTx(ctx, func(q store.Querier) error {
		if req.Project != "" {
			if _, err := store.GetProject(ctx, q, req.Project); errors.Is(err, store.ErrNotFound) {
				return NotFound("project %s not found", req.Project)
			} else if err != nil {
				return err
			}
		}
		rows, more, err := store.BrowseTasks(ctx, q, store.TaskBrowseFilter{
			Project: req.Project, Statuses: statuses, After: after, Limit: size,
		})
		if err != nil {
			return err
		}
		for _, r := range rows {
			out.Tasks = append(out.Tasks, taskSummaryFromRow(r))
		}
		if more && len(rows) > 0 {
			last := rows[len(rows)-1]
			out.Next = encodeTaskCursor(store.TaskKey{UpdatedAt: last.UpdatedAt, ID: last.ID})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DirectiveBrowse returns every directive of req.State, active by default (CONTRACT
// §8.1). Nothing is capped and nothing is left out: an active directive past its
// review_by is listed with Expired set. Active directives are ordered by review_by
// ascending, then code; retired ones by retired_at descending, then code.
func (l *Local) DirectiveBrowse(ctx context.Context, req DirectiveBrowseRequest) (*DirectiveBrowse, error) {
	if _, err := readerFor(ctx); err != nil {
		return nil, err
	}
	state := req.State
	if state == "" {
		state = DirectiveActive
	}
	if state != DirectiveActive && state != DirectiveRetired {
		return nil, Invalid("unknown directive state %q: use active or retired", req.State)
	}
	size, err := pageSize(req.Limit, 20)
	if err != nil {
		return nil, err
	}
	after, err := decodeDirectiveCursor(req.Cursor)
	if err != nil {
		return nil, err
	}
	now := l.now()
	out := &DirectiveBrowse{Directives: []BrowsedDirective{}}
	err = l.st.WithReadTx(ctx, func(q store.Querier) error {
		rows, more, err := store.BrowseDirectives(ctx, q, store.DirectiveBrowseFilter{
			State: string(state), After: after, Limit: size,
		})
		if err != nil {
			return err
		}
		for _, r := range rows {
			d := directiveFromRow(r)
			b := BrowsedDirective{Directive: d}
			b.Expired = state == DirectiveActive && d.ReviewBy != nil && d.ReviewBy.Before(now)
			out.Directives = append(out.Directives, b)
		}
		if more && len(rows) > 0 {
			out.Next = encodeDirectiveCursor(directiveKey(state, out.Directives[len(out.Directives)-1].Directive))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// directiveKey returns the browse position of d in the list of state: the time that list
// is ordered by, and the code. It is the key the next page starts after.
func directiveKey(state DirectiveStatus, d Directive) store.DirectiveKey {
	k := store.DirectiveKey{Code: d.Code}
	if state == DirectiveRetired {
		if d.RetiredAt != nil {
			k.At = *d.RetiredAt
		}
		return k
	}
	if d.ReviewBy != nil {
		k.At = *d.ReviewBy
	}
	return k
}

// Dashboard returns the figures of CONTRACT §8.1 from one read snapshot. The clock is read
// once, when the read starts. The windows are these:
//
//   - quality_30d: the tasks with status done or failed whose closed_at is in
//     [now − 30 days, now);
//   - monthly: the six UTC calendar months ending with the current one, oldest first,
//     each [first day 00:00 UTC, next month's first day 00:00 UTC). A month's closes are
//     the done or failed tasks whose closed_at is in it, as quality_30d counts them;
//   - the expired directives: the active ones whose review_by is before now, as
//     directive_list counts them (CONTRACT §9 item 46).
//
// The dashboard creates no project row.
func (l *Local) Dashboard(ctx context.Context) (*Dashboard, error) {
	if _, err := readerFor(ctx); err != nil {
		return nil, err
	}
	now := l.now()
	out := &Dashboard{
		Running: DashboardRunning{Tasks: []TaskSummary{}},
		Monthly: make([]DashboardMonth, 0, 6),
	}
	err := l.st.WithReadTx(ctx, func(q store.Querier) error {
		running, err := store.CountTasksByStatus(ctx, q, string(TaskActive))
		if err != nil {
			return err
		}
		out.Running.Count = running
		top, _, err := store.BrowseTasks(ctx, q, store.TaskBrowseFilter{
			Statuses: []string{string(TaskActive)}, Limit: dashboardRunningLimit,
		})
		if err != nil {
			return err
		}
		for _, r := range top {
			out.Running.Tasks = append(out.Running.Tasks, taskSummaryFromRow(r))
		}

		if out.Waiting.ProposedLearnings, err = store.CountLearningsByStatus(ctx, q, string(LearningProposed)); err != nil {
			return err
		}
		if out.Waiting.BlockedTasks, err = store.CountTasksByStatus(ctx, q, string(TaskBlocked)); err != nil {
			return err
		}

		totals, err := store.DirectiveTotalsAt(ctx, q, now)
		if err != nil {
			return err
		}
		out.Waiting.ExpiredDirectives = totals.Expired
		out.Directives.Active = totals.Active
		out.Directives.Expired = totals.Expired
		if totals.OldestReviewBy != nil {
			out.Directives.OldestReviewBy = totals.OldestReviewBy.UTC().Format("2006-01-02")
		}
		if totals.EarliestRatified != nil {
			age := int(now.Sub(*totals.EarliestRatified) / (24 * time.Hour))
			out.Directives.OldestAgeDays = &age
		}

		from := now.AddDate(0, 0, -dashboardWindowDays)
		quality, err := store.CloseStatsBetween(ctx, q, from, now)
		if err != nil {
			return err
		}
		out.Quality = DashboardQuality{
			Closed: quality.Closed, Done: quality.Done, CriteriaMet: quality.CriteriaMet,
			CriteriaTotal: quality.CriteriaTotal, HighImpactMistakes: quality.HighImpactMistakes,
		}

		first := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		for back := 5; back >= 0; back-- {
			start := first.AddDate(0, -back, 0)
			end := start.AddDate(0, 1, 0)
			closes, err := store.CloseStatsBetween(ctx, q, start, end)
			if err != nil {
				return err
			}
			ratified, err := store.CountDirectivesRatifiedBetween(ctx, q, start, end)
			if err != nil {
				return err
			}
			out.Monthly = append(out.Monthly, DashboardMonth{
				Month:  start.Format("2006-01"),
				Closed: closes.Closed, Done: closes.Done,
				HighImpactMistakes: closes.HighImpactMistakes, DirectivesRatified: ratified,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
