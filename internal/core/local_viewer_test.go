package core_test

import (
	"context"
	"errors"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// TestViewerReadOfNewProjectIsNotFoundAndWritesNothing checks CONTRACT §9 item 44.
// A viewer's read of a project with no row is not_found, and it writes no row, so a
// viewer key cannot mint project rows. An agent's read of the same project creates the
// row as before, and the viewer reads it once it exists.
func TestViewerReadOfNewProjectIsNotFoundAndWritesNothing(t *testing.T) {
	s, st := openLocal(t)
	viewer := core.WithCaller(context.Background(), core.Caller{Project: "fresh-project", Role: core.RoleViewer})

	_, err := s.TaskCurrent(viewer)
	var ce *core.Error
	if !errors.As(err, &ce) || ce.Kind != core.KindNotFound {
		t.Fatalf("viewer read of a new project: err = %v, want not_found", err)
	}
	if _, err := store.GetProject(context.Background(), st, "fresh-project"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("viewer read wrote a project row: GetProject err = %v, want not found", err)
	}

	if _, err := s.TaskCurrent(agent("fresh-project", "main")); err != nil {
		t.Fatalf("agent read of a new project: %v", err)
	}
	if _, err := s.TaskCurrent(viewer); err != nil {
		t.Fatalf("viewer read of a project with a row: %v", err)
	}
}
