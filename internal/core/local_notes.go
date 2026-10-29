package core

import (
	"context"
	"errors"

	"github.com/djm56/cairn-memory/internal/store"
)

// noteFromRow converts a stored note to the core form.
func noteFromRow(r store.NoteRow) Note {
	return Note{
		ID: r.ID, TaskID: r.TaskID, Project: r.Project, Title: r.Title,
		Body: r.Body, CreatedBy: r.CreatedBy, UpdatedAt: r.UpdatedAt,
	}
}

// NotePut creates a note, or replaces the title and body of note req.ID. A new
// note may name a task of the caller's project, and is otherwise not attached
// to one. A replacement keeps the task and the creator. A note is not refused
// on a closed task, because CONTRACT §1.5 names no note among the writes a
// terminal task refuses.
//
// The request carries task_id only when it creates, so a call that gives both
// an id and a task_id is invalid (CONTRACT §7).
func (l *Local) NotePut(ctx context.Context, req NotePutRequest) (*Note, error) {
	c, err := callerFor(ctx, true)
	if err != nil {
		return nil, err
	}
	if req.ID < 0 {
		return nil, Invalid("a note id cannot be negative")
	}
	if req.ID != 0 && req.TaskID != "" {
		return nil, Invalid("task_id is set only when creating a note; to replace a note, give its id alone")
	}
	if blank(req.Title) {
		return nil, Invalid("a note needs a title")
	}
	if blank(req.Body) {
		return nil, Invalid("a note needs a body")
	}
	var out Note
	err = l.st.WithTx(ctx, func(q store.Querier) error {
		now := l.now()
		if _, err := ensureProject(ctx, q, c.Project, now); err != nil {
			return err
		}
		if req.ID != 0 {
			row, err := store.GetNote(ctx, q, c.Project, req.ID)
			if errors.Is(err, store.ErrNotFound) {
				return NotFound("note %d not found", req.ID)
			}
			if err != nil {
				return err
			}
			row.Title, row.Body, row.UpdatedAt = req.Title, req.Body, now
			if err := store.UpdateNote(ctx, q, row); err != nil {
				return err
			}
			out = noteFromRow(row)
			return nil
		}
		if req.TaskID != "" {
			if _, err := loadTask(ctx, q, c.Project, req.TaskID); err != nil {
				return err
			}
		}
		row := store.NoteRow{
			TaskID: req.TaskID, Project: c.Project, Title: req.Title, Body: req.Body,
			CreatedBy: c.Agent, UpdatedAt: now,
		}
		id, err := store.InsertNote(ctx, q, row)
		if err != nil {
			return err
		}
		row.ID = id
		out = noteFromRow(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// NoteGet returns one note of the caller's project. A note of another project
// is not_found (CONTRACT §1.2).
func (l *Local) NoteGet(ctx context.Context, req NoteGetRequest) (*Note, error) {
	c, err := callerFor(ctx, false)
	if err != nil {
		return nil, err
	}
	if req.ID <= 0 {
		return nil, Invalid("a note id is required")
	}
	if err := l.ensureReadProject(ctx, c.Project); err != nil {
		return nil, err
	}
	row, err := store.GetNote(ctx, l.st, c.Project, req.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, NotFound("note %d not found", req.ID)
	}
	if err != nil {
		return nil, err
	}
	n := noteFromRow(row)
	return &n, nil
}
