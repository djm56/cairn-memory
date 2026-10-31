package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// directiveColumns is the column list of every directive read, in
// scanDirective's order.
const directiveColumns = `id, code, scope, project, agent, title, applies_when, rule,
	rationale, reviewer_check, example, status, source_learning, ratified_by,
	ratified_at, retired_at, review_by`

// scanDirective reads one row in directiveColumns order.
func scanDirective(r rowScanner) (DirectiveRow, error) {
	var d DirectiveRow
	var project, agent, appliesWhen, rationale, reviewerCheck, example sql.NullString
	var sourceLearning sql.NullInt64
	var ratifiedBy, ratifiedAt, retiredAt, reviewBy sql.NullString
	err := r.Scan(&d.ID, &d.Code, &d.Scope, &project, &agent, &d.Title, &appliesWhen,
		&d.Rule, &rationale, &reviewerCheck, &example, &d.Status, &sourceLearning,
		&ratifiedBy, &ratifiedAt, &retiredAt, &reviewBy)
	if err != nil {
		return d, err
	}
	d.Project = nullString(project)
	d.Agent = nullString(agent)
	d.AppliesWhen = nullString(appliesWhen)
	d.Rationale = nullString(rationale)
	d.ReviewerCheck = nullString(reviewerCheck)
	d.Example = nullString(example)
	if sourceLearning.Valid {
		v := sourceLearning.Int64
		d.SourceLearning = &v
	}
	d.RatifiedBy = nullString(ratifiedBy)
	if d.RatifiedAt, err = nullTime(ratifiedAt); err != nil {
		return d, fmt.Errorf("directive %s ratified_at: %w", d.Code, err)
	}
	if d.RetiredAt, err = nullTime(retiredAt); err != nil {
		return d, fmt.Errorf("directive %s retired_at: %w", d.Code, err)
	}
	if d.ReviewBy, err = nullTime(reviewBy); err != nil {
		return d, fmt.Errorf("directive %s review_by: %w", d.Code, err)
	}
	return d, nil
}

// NextDirectiveCode returns the code for the next directive, D-0001 for the
// first, then D-0002 and so on (CONTRACT §1.3). The codes are global and count
// on from the highest one used, retired directives included, so a code is never
// reused.
//
// Read function: call it on s to show the value. To allocate a code for an
// insert, call it inside the WithTx that does the InsertDirective.
func NextDirectiveCode(ctx context.Context, q Querier) (string, error) {
	var next int
	err := q.QueryRowContext(ctx, `SELECT COALESCE(MAX(CAST(substr(code, 3) AS INTEGER)), 0) + 1
		FROM directives`).Scan(&next)
	if err != nil {
		return "", fmt.Errorf("store: next directive code: %w", err)
	}
	return fmt.Sprintf("D-%04d", next), nil
}

// InsertDirective writes a new directive and returns its id. The caller sets
// the code from NextDirectiveCode, inside the same transaction. The insert fails
// if the code is taken.
//
// Write function: call it through WithTx.
func InsertDirective(ctx context.Context, q Querier, d DirectiveRow) (int64, error) {
	res, err := q.ExecContext(ctx, `INSERT INTO directives
		(code, scope, project, agent, title, applies_when, rule, rationale,
		 reviewer_check, example, status, source_learning, ratified_by, ratified_at, retired_at,
		 review_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.Code, d.Scope, textOrNil(d.Project), textOrNil(d.Agent), d.Title,
		textOrNil(d.AppliesWhen), d.Rule, textOrNil(d.Rationale), textOrNil(d.ReviewerCheck),
		textOrNil(d.Example), d.Status, d.SourceLearning, textOrNil(d.RatifiedBy),
		timeOrNil(d.RatifiedAt), timeOrNil(d.RetiredAt), timeOrNil(d.ReviewBy))
	if err != nil {
		return 0, fmt.Errorf("store: insert directive %s: %w", d.Code, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: insert directive %s: last id: %w", d.Code, err)
	}
	return id, nil
}

// GetDirectiveByCode returns the directive with the given code, whatever its
// status. Directive codes are global, so there is no project to scope by. It
// returns ErrNotFound (wrapped) when there is none.
//
// Read function: call it on s, outside a transaction.
func GetDirectiveByCode(ctx context.Context, q Querier, code string) (DirectiveRow, error) {
	row := q.QueryRowContext(ctx, `SELECT `+directiveColumns+` FROM directives WHERE code = ?`, code)
	d, err := scanDirective(row)
	if errors.Is(err, sql.ErrNoRows) {
		return d, fmt.Errorf("store: directive %s: %w", code, ErrNotFound)
	}
	if err != nil {
		return d, fmt.Errorf("store: get directive %s: %w", code, err)
	}
	return d, nil
}

// GetDirectiveByID returns the directive with row id id, whatever its status. A
// search hit names a directive by its row id, and the hit reports its code. It
// returns ErrNotFound (wrapped) when there is none.
//
// Read function: call it on s, outside a transaction.
func GetDirectiveByID(ctx context.Context, q Querier, id int64) (DirectiveRow, error) {
	row := q.QueryRowContext(ctx, `SELECT `+directiveColumns+` FROM directives WHERE id = ?`, id)
	d, err := scanDirective(row)
	if errors.Is(err, sql.ErrNoRows) {
		return d, fmt.Errorf("store: directive row %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return d, fmt.Errorf("store: get directive row %d: %w", id, err)
	}
	return d, nil
}

// ListActiveDirectives returns the active directives that apply to a caller,
// ordered by code (CONTRACT §5). It takes:
//   - every global directive;
//   - the project directives of project;
//   - the agent directives for agent, or every agent directive when agent is "".
//
// Agent directives apply in every project (CONTRACT §9, item 11), so they are
// not filtered by project. Retired directives are never returned.
//
// Read function: call it on s, outside a transaction.
func ListActiveDirectives(ctx context.Context, q Querier, project, agent string) ([]DirectiveRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+directiveColumns+` FROM directives
		WHERE status = 'active' AND (
			scope = 'global'
			OR (scope = 'project' AND project = ?)
			OR (scope = 'agent' AND (? = '' OR agent = ?)))
		ORDER BY code`, project, agent, agent)
	if err != nil {
		return nil, fmt.Errorf("store: list directives: %w", err)
	}
	defer rows.Close()
	var out []DirectiveRow
	for rows.Next() {
		d, err := scanDirective(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list directives: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list directives: %w", err)
	}
	return out, nil
}

// UpdateDirectiveStatus sets the status and retired_at of the directive with
// the given code. It is how a directive is retired (CONTRACT §8). It returns
// ErrNotFound (wrapped) when no directive has the code.
//
// Write function: call it through WithTx. Operator calls only (CONTRACT §8);
// the store does not check who calls.
func UpdateDirectiveStatus(ctx context.Context, q Querier, code, status string, retiredAt *time.Time) error {
	res, err := q.ExecContext(ctx, `UPDATE directives SET status = ?, retired_at = ?
		WHERE code = ?`, status, timeOrNil(retiredAt), code)
	if err != nil {
		return fmt.Errorf("store: update directive %s: %w", code, err)
	}
	return requireOneRow(res, fmt.Sprintf("directive %s", code))
}
