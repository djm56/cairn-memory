package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// The reads of the web UI (CONTRACT §8.1, §9 item 51). They list across projects, and
// the dashboard figures count across them. Core calls them inside one read transaction,
// so each answer is one snapshot. Every function here is a read: none writes.

// scanProject reads one row in the column order of GetProject.
func scanProject(r rowScanner) (ProjectRow, error) {
	var p ProjectRow
	var display sql.NullString
	var created, seen string
	if err := r.Scan(&p.Name, &display, &created, &seen, &p.Settings); err != nil {
		return p, err
	}
	p.DisplayName = nullString(display)
	var err error
	if p.CreatedAt, err = parseTime(created); err != nil {
		return p, fmt.Errorf("store: project %s created_at: %w", p.Name, err)
	}
	if p.LastSeenAt, err = parseTime(seen); err != nil {
		return p, fmt.Errorf("store: project %s last_seen_at: %w", p.Name, err)
	}
	return p, nil
}

// ListProjects returns up to limit projects in name order, and reports whether more
// follow. after, when set, starts the list below that name (keyset paging). limit must
// be at least 1; core checks the caller's limit before it gets here.
//
// Read function: call it on s, or inside a read transaction.
func ListProjects(ctx context.Context, q Querier, after *string, limit int) ([]ProjectRow, bool, error) {
	if limit < 1 {
		return nil, false, fmt.Errorf("store: list projects: limit %d is below 1", limit)
	}
	query := `SELECT name, display_name, created_at, last_seen_at, settings FROM projects`
	var args []any
	if after != nil {
		query += ` WHERE name > ?`
		args = append(args, *after)
	}
	query += ` ORDER BY name LIMIT ?`
	args = append(args, limit+1)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: list projects: %w", err)
	}
	defer rows.Close()
	var out []ProjectRow
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, false, fmt.Errorf("store: list projects: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: list projects: %w", err)
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, nil
}

// TaskBrowseFilter narrows BrowseTasks. Project, when set, keeps one project's tasks;
// empty lists every project. Statuses is a list, and empty means every status. After,
// when set, starts the list below that key (keyset paging, most recently updated first).
// Limit is the page size and must be at least 1.
type TaskBrowseFilter struct {
	Project  string
	Statuses []string
	After    *TaskKey
	Limit    int
}

// BrowseTasks returns up to f.Limit tasks that match f, most recently updated first with
// id descending breaking a tie, and reports whether more follow. It is ListTasks without
// the project scope: the web UI lists every project's tasks (CONTRACT §8.1).
//
// Read function: call it on s, or inside a read transaction.
func BrowseTasks(ctx context.Context, q Querier, f TaskBrowseFilter) ([]TaskRow, bool, error) {
	if f.Limit < 1 {
		return nil, false, fmt.Errorf("store: browse tasks: limit %d is below 1", f.Limit)
	}
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE 1 = 1`
	var args []any
	if f.Project != "" {
		query += ` AND project = ?`
		args = append(args, f.Project)
	}
	if len(f.Statuses) > 0 {
		query += ` AND status IN (` + placeholders(len(f.Statuses)) + `)`
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if f.After != nil {
		query += ` AND (updated_at, id) < (?, ?)`
		args = append(args, formatTime(f.After.UpdatedAt), f.After.ID)
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, f.Limit+1)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: browse tasks: %w", err)
	}
	defer rows.Close()
	var out []TaskRow
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, false, fmt.Errorf("store: browse tasks: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: browse tasks: %w", err)
	}
	more := len(out) > f.Limit
	if more {
		out = out[:f.Limit]
	}
	return out, more, nil
}

// CountTasksByStatus returns how many tasks in every project have status.
//
// Read function: call it on s, or inside a read transaction.
func CountTasksByStatus(ctx context.Context, q Querier, status string) (int, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE status = ?`, status).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count tasks %s: %w", status, err)
	}
	return n, nil
}

// CountLearningsByStatus returns how many learnings in every project have status.
//
// Read function: call it on s, or inside a read transaction.
func CountLearningsByStatus(ctx context.Context, q Querier, status string) (int, error) {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM learnings WHERE status = ?`, status).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count learnings %s: %w", status, err)
	}
	return n, nil
}

// DirectiveTotals are the figures the dashboard takes from the active directives.
type DirectiveTotals struct {
	// Active counts the directives with status active, expired ones included.
	Active int
	// Expired counts the active directives whose review_by is before the clock.
	Expired int
	// OldestReviewBy is the earliest review_by among the active directives, or nil when
	// there is none.
	OldestReviewBy *time.Time
	// EarliestRatified is the earliest ratified_at among the active directives, or nil
	// when there is none.
	EarliestRatified *time.Time
}

// DirectiveTotalsAt returns the totals of the active directives, with an expired one
// those whose review_by is before now.
//
// Read function: call it on s, or inside a read transaction.
func DirectiveTotalsAt(ctx context.Context, q Querier, now time.Time) (DirectiveTotals, error) {
	var t DirectiveTotals
	var oldest, ratified sql.NullString
	err := q.QueryRowContext(ctx, `SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN review_by < ? THEN 1 ELSE 0 END), 0),
			MIN(review_by), MIN(ratified_at)
		FROM directives WHERE status = 'active'`, formatTime(now)).
		Scan(&t.Active, &t.Expired, &oldest, &ratified)
	if err != nil {
		return t, fmt.Errorf("store: directive totals: %w", err)
	}
	if t.OldestReviewBy, err = nullTime(oldest); err != nil {
		return t, fmt.Errorf("store: directive totals review_by: %w", err)
	}
	if t.EarliestRatified, err = nullTime(ratified); err != nil {
		return t, fmt.Errorf("store: directive totals ratified_at: %w", err)
	}
	return t, nil
}

// CountDirectivesRatifiedBetween returns how many directives, of any status, were
// ratified in [from, to).
//
// Read function: call it on s, or inside a read transaction.
func CountDirectivesRatifiedBetween(ctx context.Context, q Querier, from, to time.Time) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM directives
		WHERE ratified_at >= ? AND ratified_at < ?`, formatTime(from), formatTime(to)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count directives ratified: %w", err)
	}
	return n, nil
}

// CloseStats are the figures of the tasks closed in a window.
type CloseStats struct {
	// Closed counts the tasks with status done or failed.
	Closed int
	// Done counts those with status done.
	Done int
	// CriteriaMet counts the criterion results of met, among all the criterion results
	// recorded at those closes.
	CriteriaMet int
	// CriteriaTotal counts every criterion result recorded at those closes.
	CriteriaTotal int
	// HighImpactMistakes counts the mistakes with impact high recorded at those closes.
	HighImpactMistakes int
}

// CloseStatsBetween returns the figures of the tasks with status done or failed whose
// closed_at is in [from, to). The bounds are the caller's: core computes them.
//
// Read function: call it on s, or inside a read transaction.
func CloseStatsBetween(ctx context.Context, q Querier, from, to time.Time) (CloseStats, error) {
	var s CloseStats
	err := q.QueryRowContext(ctx, `SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN t.status = 'done' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM((SELECT COUNT(*) FROM task_criteria c
				WHERE c.task_id = t.id AND c.result = 'met')), 0),
			COALESCE(SUM((SELECT COUNT(*) FROM task_criteria c
				WHERE c.task_id = t.id AND c.result IS NOT NULL)), 0),
			COALESCE(SUM((SELECT COUNT(*) FROM task_mistakes m
				WHERE m.task_id = t.id AND m.impact = 'high')), 0)
		FROM tasks t
		WHERE t.status IN ('done', 'failed') AND t.closed_at >= ? AND t.closed_at < ?`,
		formatTime(from), formatTime(to)).
		Scan(&s.Closed, &s.Done, &s.CriteriaMet, &s.CriteriaTotal, &s.HighImpactMistakes)
	if err != nil {
		return s, fmt.Errorf("store: close stats: %w", err)
	}
	return s, nil
}

// DirectiveKey is the position of the last row of a page in a browse list: the time
// that state's list is ordered by (review_by for active, retired_at for retired), and
// the code. The next page starts after it.
type DirectiveKey struct {
	At   time.Time
	Code string
}

// DirectiveBrowseFilter narrows BrowseDirectives. State is active or retired. After,
// when set, starts the list below that key. Limit is the page size and must be at least 1.
type DirectiveBrowseFilter struct {
	State string
	After *DirectiveKey
	Limit int
}

// BrowseDirectives returns up to f.Limit directives of the state, and reports whether
// more follow. Active directives are ordered by review_by ascending, then code. Retired
// ones are ordered by retired_at descending, then code. Nothing is capped and no
// expired directive is left out: the caller reads each one's review_by. A state other
// than active or retired is an error.
//
// Read function: call it on s, or inside a read transaction.
func BrowseDirectives(ctx context.Context, q Querier, f DirectiveBrowseFilter) ([]DirectiveRow, bool, error) {
	if f.Limit < 1 {
		return nil, false, fmt.Errorf("store: browse directives: limit %d is below 1", f.Limit)
	}
	var query string
	switch f.State {
	case "active":
		query = `SELECT ` + directiveColumns + ` FROM directives WHERE status = 'active'`
		if f.After != nil {
			query += ` AND (review_by > ? OR (review_by = ? AND code > ?))`
		}
		query += ` ORDER BY review_by ASC, code ASC LIMIT ?`
	case "retired":
		query = `SELECT ` + directiveColumns + ` FROM directives WHERE status = 'retired'`
		if f.After != nil {
			query += ` AND (retired_at < ? OR (retired_at = ? AND code > ?))`
		}
		query += ` ORDER BY retired_at DESC, code ASC LIMIT ?`
	default:
		return nil, false, fmt.Errorf("store: browse directives: state %q is not active or retired", f.State)
	}
	var args []any
	if f.After != nil {
		at := formatTime(f.After.At)
		args = append(args, at, at, f.After.Code)
	}
	args = append(args, f.Limit+1)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: browse directives: %w", err)
	}
	defer rows.Close()
	var out []DirectiveRow
	for rows.Next() {
		d, err := scanDirective(rows)
		if err != nil {
			return nil, false, fmt.Errorf("store: browse directives: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: browse directives: %w", err)
	}
	more := len(out) > f.Limit
	if more {
		out = out[:f.Limit]
	}
	return out, more, nil
}
