package core_test

import (
	"context"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestNoteShapeAndTaskRules checks the rules the note/put_and_get case does not
// reach: a note with no task, a replacement that names a task, and a note id
// that is not in the caller's project.
func TestNoteShapeAndTaskRules(t *testing.T) {
	s, _ := openLocal(t)
	ctx := agent("acme-api", "main")

	loose, err := s.NotePut(ctx, core.NotePutRequest{Title: "Loose", Body: "not on a task"})
	if err != nil {
		t.Fatal(err)
	}
	if loose.TaskID != "" || loose.Project != "acme-api" || loose.CreatedBy != "main" {
		t.Errorf("note without a task = %+v", loose)
	}

	// A replacement names its note by id alone; a task_id with it is invalid.
	_, err = s.NotePut(ctx, core.NotePutRequest{ID: loose.ID, TaskID: "task-20261029-01", Title: "x", Body: "y"})
	if core.KindOf(err) != core.KindInvalid {
		t.Errorf("replacement with a task_id: error %v, want invalid", err)
	}
	_, err = s.NotePut(ctx, core.NotePutRequest{Title: " ", Body: "y"})
	if core.KindOf(err) != core.KindInvalid {
		t.Errorf("blank title: error %v, want invalid", err)
	}
	_, err = s.NotePut(ctx, core.NotePutRequest{Title: "x", Body: ""})
	if core.KindOf(err) != core.KindInvalid {
		t.Errorf("blank body: error %v, want invalid", err)
	}
	_, err = s.NotePut(ctx, core.NotePutRequest{ID: 999999, Title: "x", Body: "y"})
	if core.KindOf(err) != core.KindNotFound {
		t.Errorf("replacing an unknown note: error %v, want not_found", err)
	}

	// A note of this project that is not on a task still reads back.
	got, err := s.NoteGet(ctx, core.NoteGetRequest{ID: loose.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "not on a task" {
		t.Errorf("note body = %q", got.Body)
	}
	_, err = s.NoteGet(ctx, core.NoteGetRequest{ID: 0})
	if core.KindOf(err) != core.KindInvalid {
		t.Errorf("NoteGet with no id: error %v, want invalid", err)
	}
	_, err = s.NoteGet(context.Background(), core.NoteGetRequest{ID: loose.ID})
	if core.KindOf(err) != core.KindInvalid {
		t.Errorf("NoteGet without a caller: error %v, want invalid", err)
	}
}
