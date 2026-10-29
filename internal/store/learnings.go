package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// learningColumns is the column list of every learning read, in scanLearning's
// order.
const learningColumns = `id, project, task_id, agent, kind, title, body, target,
	rationale, example, status, superseded_by, decided_by, decided_at, decision_note, created_at`

// LearningFilter narrows ListLearnings. Project scopes the list to one project
// unless AllProjects is set (CONTRACT §5). Statuses and Kinds are lists, and
// empty means every value. Target, when set, matches exactly. Before, when set,
// starts the list below that id (keyset paging, newest first). Limit is the page
// size and must be at least 1.
type LearningFilter struct {
	Project     string
	AllProjects bool
	Statuses    []string
	Kinds       []string
	Target      string
	Before      *int64
	Limit       int
}

// scanLearning reads one row in learningColumns order.
func scanLearning(r rowScanner) (LearningRow, error) {
	var l LearningRow
	var taskID, target, rationale, example, decidedBy, decisionNote sql.NullString
	var supersededBy sql.NullInt64
	var decidedAt sql.NullString
	var created string
	err := r.Scan(&l.ID, &l.Project, &taskID, &l.Agent, &l.Kind, &l.Title, &l.Body,
		&target, &rationale, &example, &l.Status, &supersededBy, &decidedBy,
		&decidedAt, &decisionNote, &created)
	if err != nil {
		return l, err
	}
	l.TaskID = nullString(taskID)
	l.Target = nullString(target)
	l.Rationale = nullString(rationale)
	l.Example = nullString(example)
	if supersededBy.Valid {
		v := supersededBy.Int64
		l.SupersededBy = &v
	}
	l.DecidedBy = nullString(decidedBy)
	l.DecisionNote = nullString(decisionNote)
	if l.DecidedAt, err = nullTime(decidedAt); err != nil {
		return l, fmt.Errorf("learning %d decided_at: %w", l.ID, err)
	}
	if l.CreatedAt, err = parseTime(created); err != nil {
		return l, fmt.Errorf("learning %d created_at: %w", l.ID, err)
	}
	return l, nil
}

// InsertLearning writes a new learning and returns its id. The caller sets the
// status (always proposed for an agent call; CONTRACT §5).
//
// Write function: call it through WithTx.
func InsertLearning(ctx context.Context, q Querier, l LearningRow) (int64, error) {
	res, err := q.ExecContext(ctx, `INSERT INTO learnings
		(project, task_id, agent, kind, title, body, target, rationale, example,
		 status, superseded_by, decided_by, decided_at, decision_note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		l.Project, textOrNil(l.TaskID), l.Agent, l.Kind, l.Title, l.Body,
		textOrNil(l.Target), textOrNil(l.Rationale), textOrNil(l.Example), l.Status,
		l.SupersededBy, textOrNil(l.DecidedBy), timeOrNil(l.DecidedAt),
		textOrNil(l.DecisionNote), formatTime(l.CreatedAt))
	if err != nil {
		return 0, fmt.Errorf("store: insert learning: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: insert learning: last id: %w", err)
	}
	return id, nil
}

// GetLearning returns learning id, but only when it belongs to project. A
// learning of another project returns ErrNotFound (wrapped), as CONTRACT §1.2
// requires for agent calls.
//
// Read function: call it on s, outside a transaction.
func GetLearning(ctx context.Context, q Querier, project string, id int64) (LearningRow, error) {
	row := q.QueryRowContext(ctx, `SELECT `+learningColumns+` FROM learnings
		WHERE id = ? AND project = ?`, id, project)
	l, err := scanLearning(row)
	if errors.Is(err, sql.ErrNoRows) {
		return l, fmt.Errorf("store: learning %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return l, fmt.Errorf("store: get learning %d: %w", id, err)
	}
	return l, nil
}

// GetLearningAnyProject returns learning id from whatever project holds it. It
// is for operator calls, which act across projects (CONTRACT §8). Agent calls
// use GetLearning.
//
// Read function: call it on s, outside a transaction.
func GetLearningAnyProject(ctx context.Context, q Querier, id int64) (LearningRow, error) {
	row := q.QueryRowContext(ctx, `SELECT `+learningColumns+` FROM learnings WHERE id = ?`, id)
	l, err := scanLearning(row)
	if errors.Is(err, sql.ErrNoRows) {
		return l, fmt.Errorf("store: learning %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return l, fmt.Errorf("store: get learning %d: %w", id, err)
	}
	return l, nil
}

// UpdateLearning writes the mutable columns of the learning whose id matches l:
// status, superseded_by, decided_by, decided_at and decision_note. Core reads the
// row first, inside the same transaction, and passes the whole row back. The
// match is on id alone, because an operator decision is made across projects.
// It returns ErrNotFound (wrapped) when no row matches. Status changes are
// operator calls only (CONTRACT §8); the store does not check who calls.
//
// Write function: call it through WithTx.
func UpdateLearning(ctx context.Context, q Querier, l LearningRow) error {
	res, err := q.ExecContext(ctx, `UPDATE learnings SET status = ?, superseded_by = ?,
		decided_by = ?, decided_at = ?, decision_note = ? WHERE id = ?`,
		l.Status, l.SupersededBy, textOrNil(l.DecidedBy), timeOrNil(l.DecidedAt),
		textOrNil(l.DecisionNote), l.ID)
	if err != nil {
		return fmt.Errorf("store: update learning %d: %w", l.ID, err)
	}
	return requireOneRow(res, fmt.Sprintf("learning %d", l.ID))
}

// ListLearnings returns up to f.Limit learnings that match f, newest first, and
// reports whether more follow.
//
// Read function: call it on s, outside a transaction.
func ListLearnings(ctx context.Context, q Querier, f LearningFilter) ([]LearningRow, bool, error) {
	if f.Limit < 1 {
		return nil, false, fmt.Errorf("store: list learnings: limit %d is below 1", f.Limit)
	}
	query := `SELECT ` + learningColumns + ` FROM learnings WHERE 1 = 1`
	var args []any
	if !f.AllProjects {
		query += ` AND project = ?`
		args = append(args, f.Project)
	}
	if len(f.Statuses) > 0 {
		query += ` AND status IN (` + placeholders(len(f.Statuses)) + `)`
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if len(f.Kinds) > 0 {
		query += ` AND kind IN (` + placeholders(len(f.Kinds)) + `)`
		for _, k := range f.Kinds {
			args = append(args, k)
		}
	}
	if f.Target != "" {
		query += ` AND target = ?`
		args = append(args, f.Target)
	}
	if f.Before != nil {
		query += ` AND id < ?`
		args = append(args, *f.Before)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, f.Limit+1)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: list learnings: %w", err)
	}
	defer rows.Close()
	var out []LearningRow
	for rows.Next() {
		l, err := scanLearning(rows)
		if err != nil {
			return nil, false, fmt.Errorf("store: list learnings: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: list learnings: %w", err)
	}
	more := len(out) > f.Limit
	if more {
		out = out[:f.Limit]
	}
	return out, more, nil
}
