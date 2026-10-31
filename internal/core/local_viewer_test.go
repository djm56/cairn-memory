package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// viewerReads are the seven read calls of CONTRACT §1, each with a request that is valid on
// its own, so the project check is what answers. Every one of them runs ensureReadProject
// before it reads, and the table checks each, so a read added later that skips the check
// fails here (6-S4).
var viewerReads = []struct {
	name string
	call func(context.Context, core.Service) error
}{
	{"task_current", func(ctx context.Context, s core.Service) error { _, err := s.TaskCurrent(ctx); return err }},
	{"task_get", func(ctx context.Context, s core.Service) error {
		_, err := s.TaskGet(ctx, core.TaskGetRequest{ID: "task-20261031-01"})
		return err
	}},
	{"task_list", func(ctx context.Context, s core.Service) error {
		_, err := s.TaskList(ctx, core.TaskListRequest{})
		return err
	}},
	{"search", func(ctx context.Context, s core.Service) error {
		_, err := s.Search(ctx, core.SearchRequest{Query: "anything"})
		return err
	}},
	{"recall", func(ctx context.Context, s core.Service) error {
		_, err := s.Recall(ctx, core.RecallRequest{Goal: "anything"})
		return err
	}},
	{"learning_list", func(ctx context.Context, s core.Service) error {
		_, err := s.LearningList(ctx, core.LearningListRequest{})
		return err
	}},
	{"directive_list", func(ctx context.Context, s core.Service) error {
		_, err := s.DirectiveList(ctx, core.DirectiveListRequest{})
		return err
	}},
}

// TestViewerReadOfNewProjectIsNotFoundAndWritesNothing checks CONTRACT §9 item 44, for every
// read. A viewer's read of a project with no row is not_found, and it writes no row, so a viewer
// key cannot mint project rows. An agent's read of the same project creates the row as before.
// Each read has its own project, so no case sees a row another case made.
func TestViewerReadOfNewProjectIsNotFoundAndWritesNothing(t *testing.T) {
	s, st := openLocal(t)
	for _, r := range viewerReads {
		project := "fresh-" + strings.ReplaceAll(r.name, "_", "-")
		viewer := core.WithCaller(context.Background(), core.Caller{Project: project, Role: core.RoleViewer})

		err := r.call(viewer, s)
		var ce *core.Error
		if !errors.As(err, &ce) || ce.Kind != core.KindNotFound {
			t.Errorf("%s: viewer read of a new project: err = %v, want not_found", r.name, err)
		}
		if _, err := store.GetProject(context.Background(), st, project); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s: viewer read wrote a project row: GetProject err = %v, want not found", r.name, err)
		}

		// The agent's read of the same project still creates the row.
		_ = r.call(agent(project, "main"), s)
		if _, err := store.GetProject(context.Background(), st, project); err != nil {
			t.Errorf("%s: agent read did not create the project row: %v", r.name, err)
		}
	}

	// Once the row exists, a viewer's read of it is answered, not refused.
	if _, err := s.TaskCurrent(core.WithCaller(context.Background(), core.Caller{Project: "fresh-task-current", Role: core.RoleViewer})); err != nil {
		t.Fatalf("viewer read of a project with a row: %v", err)
	}
}
