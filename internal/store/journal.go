package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// eventProjectRefusal is the message of the events_project_matches_task trigger
// (migration 0003). InsertEvent matches it to return ErrProjectMismatch. The two
// must stay equal; TestEventsProjectMatchesTask fails if they drift.
const eventProjectRefusal = "event project differs from the project of its task"

// EventFilter narrows ListEvents. TaskID and Project are required: a journal is
// read for one task of one project. StepID, when set, keeps only the events of
// that step. Before, when set, starts the list below that event id (keyset
// paging, newest first). Limit is the page size and must be at least 1.
type EventFilter struct {
	TaskID  string
	Project string
	StepID  *int64
	Before  *int64
	Limit   int
}

// InsertEvent appends an event and returns its id. The journal is append-only:
// there is no update or delete for events (CONTRACT §4). The search_index row
// is written by a trigger. When e.Project is not the project of e.TaskID, the
// schema refuses the row and InsertEvent returns ErrProjectMismatch (wrapped).
//
// Write function: call it through WithTx.
func InsertEvent(ctx context.Context, q Querier, e EventRow) (int64, error) {
	res, err := q.ExecContext(ctx, `INSERT INTO events
		(task_id, project, step_id, agent, session_id, kind, text, data, occurred_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.TaskID, e.Project, e.StepID, e.Agent, textOrNil(e.SessionID), e.Kind, e.Text,
		textOrNil(e.Data), formatTime(e.OccurredAt))
	if err != nil {
		if strings.Contains(err.Error(), eventProjectRefusal) {
			return 0, fmt.Errorf("store: insert event on %s: %w", e.TaskID, ErrProjectMismatch)
		}
		return 0, fmt.Errorf("store: insert event on %s: %w", e.TaskID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: insert event on %s: last id: %w", e.TaskID, err)
	}
	return id, nil
}

// ListEvents returns up to f.Limit events of one task, newest first, and
// reports whether more follow. A step filter with an id of another task matches
// nothing. The caller checks that the task exists in the project first.
//
// Read function: call it on s, outside a transaction.
func ListEvents(ctx context.Context, q Querier, f EventFilter) ([]EventRow, bool, error) {
	if f.Limit < 1 {
		return nil, false, fmt.Errorf("store: list events: limit %d is below 1", f.Limit)
	}
	query := `SELECT id, task_id, project, step_id, agent, session_id, kind, text, data, occurred_at
		FROM events WHERE task_id = ? AND project = ?`
	args := []any{f.TaskID, f.Project}
	if f.StepID != nil {
		query += ` AND step_id = ?`
		args = append(args, *f.StepID)
	}
	if f.Before != nil {
		query += ` AND id < ?`
		args = append(args, *f.Before)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, f.Limit+1)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: list events of %s: %w", f.TaskID, err)
	}
	defer rows.Close()
	var out []EventRow
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, false, fmt.Errorf("store: list events of %s: %w", f.TaskID, err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: list events of %s: %w", f.TaskID, err)
	}
	more := len(out) > f.Limit
	if more {
		out = out[:f.Limit]
	}
	return out, more, nil
}

// scanEvent reads one row in the column order of ListEvents.
func scanEvent(r rowScanner) (EventRow, error) {
	var e EventRow
	var stepID sql.NullInt64
	var session, data sql.NullString
	var occurred string
	err := r.Scan(&e.ID, &e.TaskID, &e.Project, &stepID, &e.Agent, &session,
		&e.Kind, &e.Text, &data, &occurred)
	if err != nil {
		return e, err
	}
	if stepID.Valid {
		v := stepID.Int64
		e.StepID = &v
	}
	e.SessionID = nullString(session)
	e.Data = nullString(data)
	if e.OccurredAt, err = parseTime(occurred); err != nil {
		return e, fmt.Errorf("event %d occurred_at: %w", e.ID, err)
	}
	return e, nil
}

// InsertReview records a review of a step and returns its id. Core checks that
// the step belongs to the task before it calls this.
//
// Write function: call it through WithTx.
func InsertReview(ctx context.Context, q Querier, r ReviewRow) (int64, error) {
	res, err := q.ExecContext(ctx, `INSERT INTO reviews
		(task_id, step_id, reviewer, verdict, critical, warnings, summary, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TaskID, r.StepID, r.Reviewer, r.Verdict, r.Critical, r.Warnings,
		textOrNil(r.Summary), formatTime(r.CreatedAt))
	if err != nil {
		return 0, fmt.Errorf("store: insert review of step %d: %w", r.StepID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: insert review of step %d: last id: %w", r.StepID, err)
	}
	return id, nil
}

// ListReviews returns every review of task taskID, oldest first (id order), so
// that a step's reviews read in the order they were made.
//
// Core checks the task's project first, with GetTask.
//
// Read function: call it on s, outside a transaction.
func ListReviews(ctx context.Context, q Querier, taskID string) ([]ReviewRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, task_id, step_id, reviewer, verdict,
		critical, warnings, summary, created_at
		FROM reviews WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("store: list reviews of %s: %w", taskID, err)
	}
	defer rows.Close()
	var out []ReviewRow
	for rows.Next() {
		var r ReviewRow
		var summary sql.NullString
		var created string
		if err := rows.Scan(&r.ID, &r.TaskID, &r.StepID, &r.Reviewer, &r.Verdict,
			&r.Critical, &r.Warnings, &summary, &created); err != nil {
			return nil, fmt.Errorf("store: list reviews of %s: %w", taskID, err)
		}
		r.Summary = nullString(summary)
		if r.CreatedAt, err = parseTime(created); err != nil {
			return nil, fmt.Errorf("store: review %d created_at: %w", r.ID, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list reviews of %s: %w", taskID, err)
	}
	return out, nil
}
