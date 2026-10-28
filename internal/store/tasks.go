package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// taskColumns is the column list every task read uses, in scanTask's order.
const taskColumns = `id, project, title, goal, context, constraints, status, priority,
	owner, next_action, blockers, outcome_summary, no_lessons_reason,
	opened_at, started_at, closed_at, updated_at`

// scanTask reads one task row in taskColumns order.
func scanTask(r rowScanner) (TaskRow, error) {
	var t TaskRow
	var ctxText, constraints, owner, next, blockers, outcome, noLessons sql.NullString
	var opened, updated string
	var started, closed sql.NullString
	err := r.Scan(&t.ID, &t.Project, &t.Title, &t.Goal, &ctxText, &constraints,
		&t.Status, &t.Priority, &owner, &next, &blockers, &outcome, &noLessons,
		&opened, &started, &closed, &updated)
	if err != nil {
		return t, err
	}
	t.Context = nullString(ctxText)
	t.Constraints = nullString(constraints)
	t.Owner = nullString(owner)
	t.NextAction = nullString(next)
	t.Blockers = nullString(blockers)
	t.OutcomeSummary = nullString(outcome)
	t.NoLessonsReason = nullString(noLessons)
	if t.OpenedAt, err = parseTime(opened); err != nil {
		return t, fmt.Errorf("task %s opened_at: %w", t.ID, err)
	}
	if t.StartedAt, err = nullTime(started); err != nil {
		return t, fmt.Errorf("task %s started_at: %w", t.ID, err)
	}
	if t.ClosedAt, err = nullTime(closed); err != nil {
		return t, fmt.Errorf("task %s closed_at: %w", t.ID, err)
	}
	if t.UpdatedAt, err = parseTime(updated); err != nil {
		return t, fmt.Errorf("task %s updated_at: %w", t.ID, err)
	}
	return t, nil
}

// InsertTask writes a new task row. The caller sets every field, including
// opened_at and updated_at. It returns an error when the id exists or the
// project is unknown.
//
// Write function: call it through WithTx, after AllocateTaskID for the id.
func InsertTask(ctx context.Context, q Querier, t TaskRow) error {
	_, err := q.ExecContext(ctx, `INSERT INTO tasks (`+taskColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Project, t.Title, t.Goal, textOrNil(t.Context), textOrNil(t.Constraints),
		t.Status, t.Priority, textOrNil(t.Owner), textOrNil(t.NextAction),
		textOrNil(t.Blockers), textOrNil(t.OutcomeSummary), textOrNil(t.NoLessonsReason),
		formatTime(t.OpenedAt), timeOrNil(t.StartedAt), timeOrNil(t.ClosedAt),
		formatTime(t.UpdatedAt))
	if err != nil {
		return fmt.Errorf("store: insert task %s: %w", t.ID, err)
	}
	return nil
}

// GetTask returns task id, but only when it belongs to project. A task in
// another project returns ErrNotFound (wrapped), as CONTRACT §1.2 requires.
//
// Read function: call it on s, outside a transaction.
func GetTask(ctx context.Context, q Querier, project, id string) (TaskRow, error) {
	row := q.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks
		WHERE id = ? AND project = ?`, id, project)
	t, err := scanTask(row)
	if err == sql.ErrNoRows {
		return t, fmt.Errorf("task %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return t, fmt.Errorf("store: get task %s: %w", id, err)
	}
	return t, nil
}

// GetTaskAnyProject returns task id from whatever project holds it. Only
// recall reads across projects (CONTRACT §6); every other read uses GetTask.
//
// Read function: call it on s, outside a transaction.
func GetTaskAnyProject(ctx context.Context, q Querier, id string) (TaskRow, error) {
	row := q.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id)
	t, err := scanTask(row)
	if err == sql.ErrNoRows {
		return t, fmt.Errorf("task %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return t, fmt.Errorf("store: get task %s: %w", id, err)
	}
	return t, nil
}

// UpdateTask writes every mutable column of the row whose id and project match
// t. Core reads the row with GetTask first, inside the same transaction, changes
// the fields it needs, and passes the whole row back. It returns ErrNotFound
// (wrapped) when no row matches. t.Project is never changed: the update matches
// on it, and the tasks_project_immutable trigger refuses any change to it.
//
// Write function: call it through WithTx.
func UpdateTask(ctx context.Context, q Querier, t TaskRow) error {
	res, err := q.ExecContext(ctx, `UPDATE tasks SET
		title = ?, goal = ?, context = ?, constraints = ?, status = ?, priority = ?,
		owner = ?, next_action = ?, blockers = ?, outcome_summary = ?,
		no_lessons_reason = ?, opened_at = ?, started_at = ?, closed_at = ?, updated_at = ?
		WHERE id = ? AND project = ?`,
		t.Title, t.Goal, textOrNil(t.Context), textOrNil(t.Constraints), t.Status, t.Priority,
		textOrNil(t.Owner), textOrNil(t.NextAction), textOrNil(t.Blockers),
		textOrNil(t.OutcomeSummary), textOrNil(t.NoLessonsReason), formatTime(t.OpenedAt),
		timeOrNil(t.StartedAt), timeOrNil(t.ClosedAt), formatTime(t.UpdatedAt),
		t.ID, t.Project)
	if err != nil {
		return fmt.Errorf("store: update task %s: %w", t.ID, err)
	}
	return requireOneRow(res, fmt.Sprintf("task %s", t.ID))
}

// TaskKey is the position of a task in ListTasks: its updated_at and id. Pass
// the last row of one page as the After key of the next.
type TaskKey struct {
	UpdatedAt time.Time
	ID        string
}

// ListTasks returns up to limit tasks of project, most recently updated first,
// with ties broken by id. statuses narrows the list; empty means every status.
// after, when set, starts the list below that key (keyset paging, so a new
// update does not shift the pages). It also reports whether more rows follow.
// limit must be at least 1; core checks the caller's limit before it gets here.
//
// Read function: call it on s, outside a transaction.
func ListTasks(ctx context.Context, q Querier, project string, statuses []string,
	after *TaskKey, limit int) ([]TaskRow, bool, error) {
	if limit < 1 {
		return nil, false, fmt.Errorf("store: list tasks: limit %d is below 1", limit)
	}
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE project = ?`
	args := []any{project}
	if len(statuses) > 0 {
		query += ` AND status IN (` + placeholders(len(statuses)) + `)`
		for _, s := range statuses {
			args = append(args, s)
		}
	}
	if after != nil {
		query += ` AND (updated_at, id) < (?, ?)`
		args = append(args, formatTime(after.UpdatedAt), after.ID)
	}
	query += ` ORDER BY updated_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, fmt.Errorf("store: list tasks: %w", err)
	}
	defer rows.Close()
	var out []TaskRow
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, false, fmt.Errorf("store: list tasks: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("store: list tasks: %w", err)
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, nil
}

// AllocateTaskID returns the next task id for day (UTC), of the form
// task-YYYYMMDD-NN (DESIGN §3.1, CONTRACT §1.3). The counter is daily and
// global across projects. It reads the highest number used that day and adds
// one, so it does not write anything itself.
//
// It must run inside WithTx, in the same transaction as the InsertTask that
// uses the id. The write lock held by that transaction is what makes two
// allocations return different numbers. Called outside one, two callers can
// read the same counter, and one of their inserts fails on the primary key.
func AllocateTaskID(ctx context.Context, q Querier, day time.Time) (string, error) {
	prefix := "task-" + day.UTC().Format("20060102") + "-"
	var last int
	err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(CAST(substr(id, 15) AS INTEGER)), 0)
		FROM tasks WHERE substr(id, 1, 14) = ?`, prefix).Scan(&last)
	if err != nil {
		return "", fmt.Errorf("store: allocate task id: %w", err)
	}
	return fmt.Sprintf("%s%02d", prefix, last+1), nil
}

// ListCriteria returns the acceptance criteria of task taskID, in ord order.
//
// Read function: call it on s, outside a transaction.
func ListCriteria(ctx context.Context, q Querier, taskID string) ([]CriterionRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT task_id, ord, criterion, result, evidence
		FROM task_criteria WHERE task_id = ? ORDER BY ord`, taskID)
	if err != nil {
		return nil, fmt.Errorf("store: list criteria: %w", err)
	}
	defer rows.Close()
	var out []CriterionRow
	for rows.Next() {
		var c CriterionRow
		var result, evidence sql.NullString
		if err := rows.Scan(&c.TaskID, &c.Ord, &c.Criterion, &result, &evidence); err != nil {
			return nil, fmt.Errorf("store: list criteria: %w", err)
		}
		c.Result = nullString(result)
		c.Evidence = nullString(evidence)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list criteria: %w", err)
	}
	return out, nil
}

// ReplaceCriteria deletes every criterion of task taskID and inserts rows in
// their place. The brief sets the list, and a list given replaces the whole
// list (CONTRACT §2, task_update). At close, core passes the same criteria
// back with their results filled in.
//
// Write function: call it through WithTx. The delete and the inserts must
// commit together.
func ReplaceCriteria(ctx context.Context, q Querier, taskID string, rows []CriterionRow) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM task_criteria WHERE task_id = ?`, taskID); err != nil {
		return fmt.Errorf("store: delete criteria of %s: %w", taskID, err)
	}
	for _, c := range rows {
		_, err := q.ExecContext(ctx, `INSERT INTO task_criteria
			(task_id, ord, criterion, result, evidence) VALUES (?, ?, ?, ?, ?)`,
			taskID, c.Ord, c.Criterion, textOrNil(c.Result), textOrNil(c.Evidence))
		if err != nil {
			return fmt.Errorf("store: insert criterion %d of %s: %w", c.Ord, taskID, err)
		}
	}
	return nil
}

// ListFiles returns the files of task taskID with the given role ("key" or
// "deliverable"), in ord order.
//
// Read function: call it on s, outside a transaction.
func ListFiles(ctx context.Context, q Querier, taskID, role string) ([]FileRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT task_id, role, ord, path, note
		FROM task_files WHERE task_id = ? AND role = ? ORDER BY ord`, taskID, role)
	if err != nil {
		return nil, fmt.Errorf("store: list files: %w", err)
	}
	defer rows.Close()
	var out []FileRow
	for rows.Next() {
		var f FileRow
		var note sql.NullString
		if err := rows.Scan(&f.TaskID, &f.Role, &f.Ord, &f.Path, &note); err != nil {
			return nil, fmt.Errorf("store: list files: %w", err)
		}
		f.Note = nullString(note)
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list files: %w", err)
	}
	return out, nil
}

// ReplaceFiles deletes the files of task taskID that have role, then inserts
// rows in their place. Key files and deliverables share task_files, so only
// that role's rows change. role must be "key" or "deliverable", and every row
// must carry it; a row with another role returns an error before any insert.
// An empty rows list is valid: it clears that role.
//
// Write function: call it through WithTx. The check on a row's role runs after
// the delete, so a failed call inside WithTx rolls the delete back too.
func ReplaceFiles(ctx context.Context, q Querier, taskID, role string, rows []FileRow) error {
	if role != "key" && role != "deliverable" {
		return fmt.Errorf("store: replace files: role %q is not key or deliverable", role)
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM task_files WHERE task_id = ? AND role = ?`,
		taskID, role); err != nil {
		return fmt.Errorf("store: delete %s files of %s: %w", role, taskID, err)
	}
	for _, f := range rows {
		if f.Role != role {
			return fmt.Errorf("store: replace files: row role %q, want %q", f.Role, role)
		}
		_, err := q.ExecContext(ctx, `INSERT INTO task_files (task_id, role, ord, path, note)
			VALUES (?, ?, ?, ?, ?)`, taskID, role, f.Ord, f.Path, textOrNil(f.Note))
		if err != nil {
			return fmt.Errorf("store: insert %s file %d of %s: %w", role, f.Ord, taskID, err)
		}
	}
	return nil
}

// ListLessons returns the lessons of task taskID, in ord order.
//
// Read function: call it on s, outside a transaction.
func ListLessons(ctx context.Context, q Querier, taskID string) ([]LessonRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT task_id, ord, kind, text, why, correction
		FROM task_lessons WHERE task_id = ? ORDER BY ord`, taskID)
	if err != nil {
		return nil, fmt.Errorf("store: list lessons: %w", err)
	}
	defer rows.Close()
	var out []LessonRow
	for rows.Next() {
		var l LessonRow
		var why, correction sql.NullString
		if err := rows.Scan(&l.TaskID, &l.Ord, &l.Kind, &l.Text, &why, &correction); err != nil {
			return nil, fmt.Errorf("store: list lessons: %w", err)
		}
		l.Why = nullString(why)
		l.Correction = nullString(correction)
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list lessons: %w", err)
	}
	return out, nil
}

// GetLesson returns lesson ord of task taskID. It returns ErrNotFound (wrapped)
// when there is none. Search hits name a lesson as "task-id/ord"; see
// ParseChildRef.
//
// Read function: call it on s, outside a transaction.
func GetLesson(ctx context.Context, q Querier, taskID string, ord int) (LessonRow, error) {
	var l LessonRow
	var why, correction sql.NullString
	err := q.QueryRowContext(ctx, `SELECT task_id, ord, kind, text, why, correction
		FROM task_lessons WHERE task_id = ? AND ord = ?`, taskID, ord).
		Scan(&l.TaskID, &l.Ord, &l.Kind, &l.Text, &why, &correction)
	if err == sql.ErrNoRows {
		return l, fmt.Errorf("lesson %s/%d: %w", taskID, ord, ErrNotFound)
	}
	if err != nil {
		return l, fmt.Errorf("store: get lesson %s/%d: %w", taskID, ord, err)
	}
	l.Why = nullString(why)
	l.Correction = nullString(correction)
	return l, nil
}

// ReplaceLessons deletes the lessons of task taskID and inserts rows in their
// place. Lessons are written once, at close (CONTRACT §2, task_close).
//
// Write function: call it through WithTx.
func ReplaceLessons(ctx context.Context, q Querier, taskID string, rows []LessonRow) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM task_lessons WHERE task_id = ?`, taskID); err != nil {
		return fmt.Errorf("store: delete lessons of %s: %w", taskID, err)
	}
	for _, l := range rows {
		_, err := q.ExecContext(ctx, `INSERT INTO task_lessons
			(task_id, ord, kind, text, why, correction) VALUES (?, ?, ?, ?, ?, ?)`,
			taskID, l.Ord, l.Kind, l.Text, textOrNil(l.Why), textOrNil(l.Correction))
		if err != nil {
			return fmt.Errorf("store: insert lesson %d of %s: %w", l.Ord, taskID, err)
		}
	}
	return nil
}

// ListMistakes returns the mistakes of task taskID, in ord order.
//
// Read function: call it on s, outside a transaction.
func ListMistakes(ctx context.Context, q Querier, taskID string) ([]MistakeRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT task_id, ord, agent, mistake, impact, correction, prevention
		FROM task_mistakes WHERE task_id = ? ORDER BY ord`, taskID)
	if err != nil {
		return nil, fmt.Errorf("store: list mistakes: %w", err)
	}
	defer rows.Close()
	var out []MistakeRow
	for rows.Next() {
		var m MistakeRow
		var correction, prevention sql.NullString
		if err := rows.Scan(&m.TaskID, &m.Ord, &m.Agent, &m.Mistake, &m.Impact, &correction, &prevention); err != nil {
			return nil, fmt.Errorf("store: list mistakes: %w", err)
		}
		m.Correction = nullString(correction)
		m.Prevention = nullString(prevention)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list mistakes: %w", err)
	}
	return out, nil
}

// GetMistake returns mistake ord of task taskID. It returns ErrNotFound
// (wrapped) when there is none.
//
// Read function: call it on s, outside a transaction.
func GetMistake(ctx context.Context, q Querier, taskID string, ord int) (MistakeRow, error) {
	var m MistakeRow
	var correction, prevention sql.NullString
	err := q.QueryRowContext(ctx, `SELECT task_id, ord, agent, mistake, impact, correction, prevention
		FROM task_mistakes WHERE task_id = ? AND ord = ?`, taskID, ord).
		Scan(&m.TaskID, &m.Ord, &m.Agent, &m.Mistake, &m.Impact, &correction, &prevention)
	if err == sql.ErrNoRows {
		return m, fmt.Errorf("mistake %s/%d: %w", taskID, ord, ErrNotFound)
	}
	if err != nil {
		return m, fmt.Errorf("store: get mistake %s/%d: %w", taskID, ord, err)
	}
	m.Correction = nullString(correction)
	m.Prevention = nullString(prevention)
	return m, nil
}

// ReplaceMistakes deletes the mistakes of task taskID and inserts rows in their
// place. Mistakes are written once, at close (CONTRACT §2, task_close).
//
// Write function: call it through WithTx.
func ReplaceMistakes(ctx context.Context, q Querier, taskID string, rows []MistakeRow) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM task_mistakes WHERE task_id = ?`, taskID); err != nil {
		return fmt.Errorf("store: delete mistakes of %s: %w", taskID, err)
	}
	for _, m := range rows {
		_, err := q.ExecContext(ctx, `INSERT INTO task_mistakes
			(task_id, ord, agent, mistake, impact, correction, prevention)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			taskID, m.Ord, m.Agent, m.Mistake, m.Impact, textOrNil(m.Correction), textOrNil(m.Prevention))
		if err != nil {
			return fmt.Errorf("store: insert mistake %d of %s: %w", m.Ord, taskID, err)
		}
	}
	return nil
}

// placeholders returns n comma-separated "?" markers for an IN list.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, 0, 2*n)
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}
