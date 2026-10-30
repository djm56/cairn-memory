package core_test

import (
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestTaskCurrentReadsOneSnapshot checks CONTRACT §9 item 35: task_current reads
// its task, criteria, steps, events and directives in one read transaction. So a
// write that commits during the call is seen by all of the answer or by none.
// The seam runs between the first read and the rest, and there it adds a step
// and an event. The answer must show the state from before those writes, and a
// later call must show them. A plain store read would show the step and the
// event, so this test fails if task_current stops reading through one snapshot.
func TestTaskCurrentReadsOneSnapshot(t *testing.T) {
	s, _ := openLocal(t)
	ctx := agent("acme-api", "main")
	res := must(s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g", Steps: []core.StepInput{{Title: "one"}}}))(t)
	must(s.EventLog(ctx, core.EventLogRequest{TaskID: res.ID, Kind: core.EventNote, Text: "before"}))(t)

	ran := false
	s.SetBetweenReads(func() {
		ran = true
		if _, err := s.StepAdd(ctx, core.StepAddRequest{TaskID: res.ID, Title: "late", Agent: "main"}); err != nil {
			t.Errorf("step added between the reads: %v", err)
		}
		if _, err := s.EventLog(ctx, core.EventLogRequest{TaskID: res.ID, Kind: core.EventNote, Text: "late"}); err != nil {
			t.Errorf("event logged between the reads: %v", err)
		}
	})
	cur := must(s.TaskCurrent(ctx))(t)
	s.SetBetweenReads(nil)
	if !ran {
		t.Fatal("the seam did not run, so no write landed between the reads")
	}
	if cur.Task == nil || cur.Task.ID != res.ID {
		t.Fatalf("task_current task = %+v, want %s", cur.Task, res.ID)
	}
	if len(cur.Steps) != 1 {
		t.Errorf("task_current steps = %d, want 1: the step added between the reads must not show", len(cur.Steps))
	}
	if len(cur.RecentEvents) != 1 || cur.RecentEvents[0].Text != "before" {
		t.Errorf("task_current recent events = %+v, want only %q", cur.RecentEvents, "before")
	}

	after := must(s.TaskCurrent(ctx))(t)
	if len(after.Steps) != 2 || len(after.RecentEvents) != 2 {
		t.Errorf("task_current after the writes: %d steps and %d events, want 2 and 2", len(after.Steps), len(after.RecentEvents))
	}
}
