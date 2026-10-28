package store

import (
	"context"
	"database/sql"
	"fmt"
)

// stepRanked selects a task's steps with their display number. The number is
// the step's place in (position, id) order among the steps of that task, so it
// changes when a step is inserted before it, and the id never does (DESIGN
// §3.2). Every step read is built on it. The columns follow scanStep's order.
const stepRanked = `SELECT id, task_id, ROW_NUMBER() OVER (ORDER BY position, id) AS num,
	position, title, agent, status, attempts, summary, started_at, ended_at
	FROM steps WHERE task_id = ?`

// scanStep reads one row of stepRanked.
func scanStep(r rowScanner) (StepRow, error) {
	var s StepRow
	var agent, summary, started, ended sql.NullString
	err := r.Scan(&s.ID, &s.TaskID, &s.Number, &s.Position, &s.Title, &agent,
		&s.Status, &s.Attempts, &summary, &started, &ended)
	if err != nil {
		return s, err
	}
	s.Agent = nullString(agent)
	s.Summary = nullString(summary)
	if s.StartedAt, err = nullTime(started); err != nil {
		return s, fmt.Errorf("step %d started_at: %w", s.ID, err)
	}
	if s.EndedAt, err = nullTime(ended); err != nil {
		return s, fmt.Errorf("step %d ended_at: %w", s.ID, err)
	}
	return s, nil
}

// InsertStep writes a new step at the position the caller computed (see
// StepPositionLast and StepPositionBetween). It returns the new step's id. Its
// Number on the row is ignored.
//
// Write function: call it through WithTx.
func InsertStep(ctx context.Context, q Querier, s StepRow) (int64, error) {
	res, err := q.ExecContext(ctx, `INSERT INTO steps
		(task_id, position, title, agent, status, attempts, summary, started_at, ended_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.TaskID, s.Position, s.Title, textOrNil(s.Agent), s.Status, s.Attempts,
		textOrNil(s.Summary), timeOrNil(s.StartedAt), timeOrNil(s.EndedAt))
	if err != nil {
		return 0, fmt.Errorf("store: insert step of %s: %w", s.TaskID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: insert step of %s: last id: %w", s.TaskID, err)
	}
	return id, nil
}

// ListSteps returns every step of task taskID in display order, each with its
// Number.
//
// Core checks the task's project first, with GetTask.
//
// Read function: call it on s, outside a transaction.
func ListSteps(ctx context.Context, q Querier, taskID string) ([]StepRow, error) {
	rows, err := q.QueryContext(ctx, stepRanked+` ORDER BY num`, taskID)
	if err != nil {
		return nil, fmt.Errorf("store: list steps of %s: %w", taskID, err)
	}
	defer rows.Close()
	var out []StepRow
	for rows.Next() {
		s, err := scanStep(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list steps of %s: %w", taskID, err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list steps of %s: %w", taskID, err)
	}
	return out, nil
}

// GetStep returns step id, but only when it belongs to task taskID. A step of
// another task returns ErrNotFound (wrapped). Core checks the task's project
// first, with GetTask.
//
// Read function: call it on s, outside a transaction.
func GetStep(ctx context.Context, q Querier, taskID string, id int64) (StepRow, error) {
	row := q.QueryRowContext(ctx, `SELECT * FROM (`+stepRanked+`) WHERE id = ?`, taskID, id)
	s, err := scanStep(row)
	if err == sql.ErrNoRows {
		return s, fmt.Errorf("step %d of %s: %w", id, taskID, ErrNotFound)
	}
	if err != nil {
		return s, fmt.Errorf("store: get step %d: %w", id, err)
	}
	return s, nil
}

// GetStepByNumber returns the step shown as number in task taskID. It returns
// ErrNotFound (wrapped) when the task has no step with that number.
//
// Core checks the task's project first, with GetTask.
//
// Read function: call it on s, outside a transaction.
func GetStepByNumber(ctx context.Context, q Querier, taskID string, number int) (StepRow, error) {
	row := q.QueryRowContext(ctx, `SELECT * FROM (`+stepRanked+`) WHERE num = ?`, taskID, number)
	s, err := scanStep(row)
	if err == sql.ErrNoRows {
		return s, fmt.Errorf("step %d of %s: %w", number, taskID, ErrNotFound)
	}
	if err != nil {
		return s, fmt.Errorf("store: get step number %d of %s: %w", number, taskID, err)
	}
	return s, nil
}

// UpdateStep writes every column of the row whose id and task match s. Core
// reads the step first, inside the same transaction, and passes the whole row
// back with the changes. It returns ErrNotFound (wrapped) when no row matches.
// The step's Number is not stored.
//
// Write function: call it through WithTx.
func UpdateStep(ctx context.Context, q Querier, s StepRow) error {
	res, err := q.ExecContext(ctx, `UPDATE steps SET position = ?, title = ?, agent = ?,
		status = ?, attempts = ?, summary = ?, started_at = ?, ended_at = ?
		WHERE id = ? AND task_id = ?`,
		s.Position, s.Title, textOrNil(s.Agent), s.Status, s.Attempts, textOrNil(s.Summary),
		timeOrNil(s.StartedAt), timeOrNil(s.EndedAt), s.ID, s.TaskID)
	if err != nil {
		return fmt.Errorf("store: update step %d: %w", s.ID, err)
	}
	return requireOneRow(res, fmt.Sprintf("step %d of %s", s.ID, s.TaskID))
}

// StepPositionLast returns a position after every step of task taskID, for a
// step added at the end. A task with no steps gets 1.
//
// Read function: call it on s, outside a transaction. Its result is used to
// insert, so the insert belongs in the same WithTx; otherwise two inserts can
// take the same position.
func StepPositionLast(ctx context.Context, q Querier, taskID string) (float64, error) {
	var highest sql.NullFloat64
	if err := q.QueryRowContext(ctx, `SELECT MAX(position) FROM steps WHERE task_id = ?`,
		taskID).Scan(&highest); err != nil {
		return 0, fmt.Errorf("store: last step position of %s: %w", taskID, err)
	}
	if !highest.Valid {
		return 1, nil
	}
	return highest.Float64 + 1, nil
}

// StepPositionBetween returns a position for a step inserted just after the
// step at after, and before the next step of the task. A nil after means before
// the first step. The position is the midpoint of its two neighbours, so no
// other step is renumbered in the database (DESIGN §3.2). A step inserted after
// the last one gets after+1; the first step, with nothing before it, gets half
// of the first position.
//
// Read function: call it on s, outside a transaction. As with StepPositionLast,
// the insert that uses the result belongs in the same WithTx.
func StepPositionBetween(ctx context.Context, q Querier, taskID string, after *float64) (float64, error) {
	if after == nil {
		var lowest sql.NullFloat64
		if err := q.QueryRowContext(ctx, `SELECT MIN(position) FROM steps WHERE task_id = ?`,
			taskID).Scan(&lowest); err != nil {
			return 0, fmt.Errorf("store: first step position of %s: %w", taskID, err)
		}
		if !lowest.Valid {
			return 1, nil
		}
		return lowest.Float64 / 2, nil
	}
	var next sql.NullFloat64
	if err := q.QueryRowContext(ctx, `SELECT MIN(position) FROM steps
		WHERE task_id = ? AND position > ?`, taskID, *after).Scan(&next); err != nil {
		return 0, fmt.Errorf("store: next step position of %s: %w", taskID, err)
	}
	if !next.Valid {
		return *after + 1, nil
	}
	return (*after + next.Float64) / 2, nil
}

// DeletePendingSteps removes every step of task taskID that is pending, and
// returns how many it removed. Steps in any other status stay (CONTRACT §3,
// step_plan). A pending step that an event or a review still points at cannot
// be deleted: the foreign keys refuse it, and the call returns that error. Core
// has to decide what happens to such a step before it calls this; the store
// does not detach events, because the journal is append-only (CONTRACT §4).
//
// Write function: call it through WithTx.
func DeletePendingSteps(ctx context.Context, q Querier, taskID string) (int64, error) {
	res, err := q.ExecContext(ctx, `DELETE FROM steps WHERE task_id = ? AND status = 'pending'`,
		taskID)
	if err != nil {
		return 0, fmt.Errorf("store: delete pending steps of %s: %w", taskID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete pending steps of %s: rows affected: %w", taskID, err)
	}
	return n, nil
}
