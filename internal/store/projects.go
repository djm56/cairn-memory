package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// EnsureProject creates the project row if it is absent, with settings as its
// JSON, and otherwise only moves last_seen_at on. Core calls it on every agent
// call, so a project exists from its first call (CONTRACT §1.1). Existing
// settings are never overwritten here; ProjectSettingsSet does that.
//
// Write function: call it through WithTx, or from a transaction core opened.
func EnsureProject(ctx context.Context, q Querier, p ProjectRow) error {
	_, err := q.ExecContext(ctx, `INSERT INTO projects
		(name, display_name, created_at, last_seen_at, settings)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET last_seen_at = excluded.last_seen_at`,
		p.Name, textOrNil(p.DisplayName), formatTime(p.CreatedAt),
		formatTime(p.LastSeenAt), p.Settings)
	if err != nil {
		return fmt.Errorf("store: ensure project %s: %w", p.Name, err)
	}
	return nil
}

// GetProject returns the project named name. It returns ErrNotFound (wrapped)
// when there is no such project.
//
// Read function: call it on s, outside a transaction.
func GetProject(ctx context.Context, q Querier, name string) (ProjectRow, error) {
	var p ProjectRow
	var display sql.NullString
	var created, seen string
	err := q.QueryRowContext(ctx, `SELECT name, display_name, created_at, last_seen_at, settings
		FROM projects WHERE name = ?`, name).
		Scan(&p.Name, &display, &created, &seen, &p.Settings)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("store: project %s: %w", name, ErrNotFound)
	}
	if err != nil {
		return p, fmt.Errorf("store: get project %s: %w", name, err)
	}
	p.DisplayName = nullString(display)
	if p.CreatedAt, err = parseTime(created); err != nil {
		return p, fmt.Errorf("store: project %s created_at: %w", name, err)
	}
	if p.LastSeenAt, err = parseTime(seen); err != nil {
		return p, fmt.Errorf("store: project %s last_seen_at: %w", name, err)
	}
	return p, nil
}

// UpdateProject replaces the display name and settings of the project named
// p.Name. It leaves created_at and last_seen_at alone. It returns ErrNotFound
// (wrapped) when no row matches.
//
// Write function: call it through WithTx. Core is the only caller that should
// reach it, and only for an operator call (CONTRACT §8).
func UpdateProject(ctx context.Context, q Querier, p ProjectRow) error {
	res, err := q.ExecContext(ctx, `UPDATE projects SET display_name = ?, settings = ?
		WHERE name = ?`, textOrNil(p.DisplayName), p.Settings, p.Name)
	if err != nil {
		return fmt.Errorf("store: update project %s: %w", p.Name, err)
	}
	return requireOneRow(res, fmt.Sprintf("project %s", p.Name))
}

// UpsertSession creates the session row if it is absent, and otherwise moves
// last_seen_at on. The session id comes from the client (DESIGN §2).
//
// Write function: call it through WithTx.
func UpsertSession(ctx context.Context, q Querier, s SessionRow) error {
	_, err := q.ExecContext(ctx, `INSERT INTO sessions
		(id, project, client, agent, started_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET last_seen_at = excluded.last_seen_at`,
		s.ID, s.Project, textOrNil(s.Client), textOrNil(s.Agent),
		formatTime(s.StartedAt), formatTime(s.LastSeenAt))
	if err != nil {
		return fmt.Errorf("store: upsert session %s: %w", s.ID, err)
	}
	return nil
}

// GetSession returns the session with the given id. It returns ErrNotFound
// (wrapped) when there is none.
//
// Read function: call it on s, outside a transaction.
func GetSession(ctx context.Context, q Querier, id string) (SessionRow, error) {
	var s SessionRow
	var client, agent sql.NullString
	var started, seen string
	err := q.QueryRowContext(ctx, `SELECT id, project, client, agent, started_at, last_seen_at
		FROM sessions WHERE id = ?`, id).
		Scan(&s.ID, &s.Project, &client, &agent, &started, &seen)
	if errors.Is(err, sql.ErrNoRows) {
		return s, fmt.Errorf("store: session %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return s, fmt.Errorf("store: get session %s: %w", id, err)
	}
	s.Client = nullString(client)
	s.Agent = nullString(agent)
	if s.StartedAt, err = parseTime(started); err != nil {
		return s, fmt.Errorf("store: session %s started_at: %w", id, err)
	}
	if s.LastSeenAt, err = parseTime(seen); err != nil {
		return s, fmt.Errorf("store: session %s last_seen_at: %w", id, err)
	}
	return s, nil
}

// requireOneRow turns a write that matched no row into ErrNotFound. what names
// the row for the message.
func requireOneRow(res sql.Result, what string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("store: %s: %w", what, ErrNotFound)
	}
	return nil
}
