package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
)

// seamDeadline bounds each write the seam makes. On a store with one connection, the read
// transaction holds it, so a write waits for it for ever. The deadline turns that wait
// into an error, which the test reports, instead of a hang.
const seamDeadline = 5 * time.Second

// TestTaskCurrentReadsOneSnapshot checks CONTRACT §9 item 35: task_current reads its
// directives, task, criteria, steps and events in one read transaction, so a write that
// commits during the call is seen by all of the answer or by none. The seam runs inside
// the snapshot, before the first of those reads, and there it makes one write for each of
// them: it retires a directive, changes the task's next action, replaces its criteria,
// adds a step and logs an event. The answer must show the state from before those writes,
// and a later call must show them. A read that ran outside the snapshot would show its
// write, so the test fails if any of the five reads leaves the snapshot.
//
// The baseline is taken before the seam is set, so the test does not depend on how many
// events a write journals.
func TestTaskCurrentReadsOneSnapshot(t *testing.T) {
	s, _ := openLocal(t)
	ctx := agent("acme-api", "main")
	retire := ratifyGlobal(t, s, "retired during the call")
	res := must(s.TaskStart(ctx, core.TaskStartRequest{
		Title: "t", Goal: "g", Criteria: []string{"c1"}, Steps: []core.StepInput{{Title: "one"}},
	}))(t)
	before := "before the call"
	must(s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, NextAction: &before}))(t)
	must(s.EventLog(ctx, core.EventLogRequest{TaskID: res.ID, Kind: core.EventNote, Text: "before"}))(t)

	base := must(s.TaskCurrent(ctx))(t)
	if base.Task == nil || base.Task.NextAction != before || len(base.Criteria) != 1 ||
		len(base.Steps) != 1 || len(base.Directives) != 1 || !hasEvent(base.RecentEvents, "before") {
		t.Fatalf("the baseline does not hold what the seam changes: %+v", base)
	}

	ran := false
	s.SetBetweenReads(func() {
		ran = true
		wctx, cancel := context.WithTimeout(ctx, seamDeadline)
		defer cancel()
		octx, ocancel := context.WithTimeout(opCtx(), seamDeadline)
		defer ocancel()
		if _, err := s.DirectiveRetire(octx, core.DirectiveRetireRequest{Code: retire.Code}); err != nil {
			t.Errorf("directive retired between the reads: %v", err)
		}
		late := "after the call"
		if _, err := s.TaskUpdate(wctx, core.TaskUpdateRequest{ID: res.ID, NextAction: &late}); err != nil {
			t.Errorf("next action changed between the reads: %v", err)
		}
		criteria := []string{"c2"}
		if _, err := s.TaskUpdate(wctx, core.TaskUpdateRequest{ID: res.ID, Criteria: &criteria}); err != nil {
			t.Errorf("criteria replaced between the reads: %v", err)
		}
		if _, err := s.StepAdd(wctx, core.StepAddRequest{TaskID: res.ID, Title: "late", Agent: "main"}); err != nil {
			t.Errorf("step added between the reads: %v", err)
		}
		if _, err := s.EventLog(wctx, core.EventLogRequest{TaskID: res.ID, Kind: core.EventNote, Text: "late"}); err != nil {
			t.Errorf("event logged between the reads: %v", err)
		}
	})
	cur := must(s.TaskCurrent(ctx))(t)
	s.SetBetweenReads(nil)
	if !ran {
		t.Fatal("the seam did not run, so no write landed between the reads")
	}

	if cur.Task == nil || cur.Task.ID != res.ID || cur.Task.NextAction != before {
		t.Errorf("task_current task = %+v, want next action %q: the change made between the reads must not show", cur.Task, before)
	}
	if !sameCriteria(cur.Criteria, base.Criteria) {
		t.Errorf("task_current criteria = %+v, want %+v", cur.Criteria, base.Criteria)
	}
	if len(cur.Steps) != len(base.Steps) {
		t.Errorf("task_current steps = %d, want %d: the step added between the reads must not show", len(cur.Steps), len(base.Steps))
	}
	if !hasEvent(cur.RecentEvents, "before") || hasEvent(cur.RecentEvents, "late") {
		t.Errorf("task_current recent events = %+v, want the baseline and not %q", cur.RecentEvents, "late")
	}
	if len(cur.Directives) != 1 || cur.Directives[0].Code != retire.Code {
		t.Errorf("task_current directives = %+v, want the one retired between the reads, %s", cur.Directives, retire.Code)
	}

	after := must(s.TaskCurrent(ctx))(t)
	if after.Task == nil || after.Task.NextAction != "after the call" {
		t.Errorf("task_current after the writes: next action %+v, want %q", after.Task, "after the call")
	}
	if !sameCriteria(after.Criteria, []core.Criterion{{Criterion: "c2"}}) {
		t.Errorf("task_current after the writes: criteria %+v, want c2 only", after.Criteria)
	}
	if len(after.Steps) != len(base.Steps)+1 {
		t.Errorf("task_current after the writes: %d steps, want %d", len(after.Steps), len(base.Steps)+1)
	}
	if !hasEvent(after.RecentEvents, "late") {
		t.Errorf("task_current after the writes: recent events %+v, want %q", after.RecentEvents, "late")
	}
	if len(after.Directives) != 0 {
		t.Errorf("task_current after the writes: directives %+v, want none", after.Directives)
	}
}

// hasEvent reports whether events holds one whose text is text.
func hasEvent(events []core.Event, text string) bool {
	for _, e := range events {
		if e.Text == text {
			return true
		}
	}
	return false
}

// sameCriteria reports whether a and b give the same criterion texts, in order.
func sameCriteria(a, b []core.Criterion) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Criterion != b[i].Criterion {
			return false
		}
	}
	return true
}
