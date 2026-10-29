package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// InsertNote writes a new note and returns its id. An empty TaskID stores NULL:
// the note is not attached to a task.
//
// Write function: call it through WithTx.
func InsertNote(ctx context.Context, q Querier, n NoteRow) (int64, error) {
	res, err := q.ExecContext(ctx, `INSERT INTO notes
		(task_id, project, title, body, created_by, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		textOrNil(n.TaskID), n.Project, n.Title, n.Body, n.CreatedBy, formatTime(n.UpdatedAt))
	if err != nil {
		return 0, fmt.Errorf("store: insert note: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: insert note: last id: %w", err)
	}
	return id, nil
}

// GetNote returns note id, but only when it belongs to project. A note of
// another project returns ErrNotFound (wrapped).
//
// Read function: call it on s, outside a transaction.
func GetNote(ctx context.Context, q Querier, project string, id int64) (NoteRow, error) {
	var n NoteRow
	var taskID sql.NullString
	var updated string
	err := q.QueryRowContext(ctx, `SELECT id, task_id, project, title, body, created_by, updated_at
		FROM notes WHERE id = ? AND project = ?`, id, project).
		Scan(&n.ID, &taskID, &n.Project, &n.Title, &n.Body, &n.CreatedBy, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return n, fmt.Errorf("note %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return n, fmt.Errorf("store: get note %d: %w", id, err)
	}
	n.TaskID = nullString(taskID)
	if n.UpdatedAt, err = parseTime(updated); err != nil {
		return n, fmt.Errorf("store: note %d updated_at: %w", id, err)
	}
	return n, nil
}

// UpdateNote replaces the title, body and updated_at of the note whose id and
// project match n. It returns ErrNotFound (wrapped) when no row matches. The
// task and the creator are not changed.
//
// Write function: call it through WithTx.
func UpdateNote(ctx context.Context, q Querier, n NoteRow) error {
	res, err := q.ExecContext(ctx, `UPDATE notes SET title = ?, body = ?, updated_at = ?
		WHERE id = ? AND project = ?`,
		n.Title, n.Body, formatTime(n.UpdatedAt), n.ID, n.Project)
	if err != nil {
		return fmt.Errorf("store: update note %d: %w", n.ID, err)
	}
	return requireOneRow(res, fmt.Sprintf("note %d", n.ID))
}
