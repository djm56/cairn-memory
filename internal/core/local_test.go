package core_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/conformance"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// deferred lists the conformance cases that core.Local does not yet pass. It is
// empty: every case is live. RunWith still fails a name here that is not in the
// suite, or that passes, so an entry added later cannot hide a case.
var deferred = map[string]string{}

// openLocal returns a Service over a new database in a temporary directory,
// and the store under it, for tests that check the rows directly.
func openLocal(t *testing.T) (*core.Local, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cairn.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return core.NewLocal(st), st
}

// agent returns a context for agent in project.
func agent(project, name string) context.Context {
	return core.WithCaller(context.Background(), core.Caller{Project: project, Agent: name, Role: core.RoleAgent})
}

func TestConformance(t *testing.T) {
	conformance.RunWith(t, func(t *testing.T) core.Service {
		s, _ := openLocal(t)
		return s
	}, deferred)
}

// TestStepStartAndFinishWriteEvents checks the journal rows that step_update
// writes, read back through event_list.
func TestStepStartAndFinishWriteEvents(t *testing.T) {
	s, _ := openLocal(t)
	ctx := agent("acme-api", "main")
	res, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g", Steps: []core.StepInput{{Title: "one"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(1), Action: core.StepStart}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(1), Action: core.StepFinish, Summary: "ok"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.EventList(ctx, core.EventListRequest{TaskID: res.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	// event_list is newest first.
	var kinds []string
	for _, e := range list.Events {
		kinds = append(kinds, string(e.Kind))
	}
	if len(kinds) != 2 || kinds[0] != string(core.EventFinished) || kinds[1] != string(core.EventStarted) {
		t.Errorf("journal kinds = %v, want [finished started] newest first", kinds)
	}
}

// TestStepPlanRefusesPendingStepWithRecords checks that step_plan does not
// detach a journal: a pending step with an event cannot be removed. The same
// holds for a pending step with a review.
func TestStepPlanRefusesPendingStepWithRecords(t *testing.T) {
	s, _ := openLocal(t)
	ctx := agent("acme-api", "main")
	res, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g", Steps: []core.StepInput{{Title: "one"}, {Title: "two"}, {Title: "three"}}})
	if err != nil {
		t.Fatal(err)
	}
	two := core.NumberRef(2)
	if _, err := s.EventLog(ctx, core.EventLogRequest{TaskID: res.ID, Step: &two, Kind: core.EventNote, Text: "about two"}); err != nil {
		t.Fatal(err)
	}
	three := core.NumberRef(3)
	if _, err := s.ReviewRecord(agent("acme-api", "reviewer"), core.ReviewRecordRequest{TaskID: res.ID, Step: three, Verdict: core.VerdictApproved}); err != nil {
		t.Fatal(err)
	}
	_, err = s.StepPlan(ctx, core.StepPlanRequest{TaskID: res.ID, Steps: []core.StepInput{{Title: "new"}}})
	if core.KindOf(err) != core.KindRefused {
		t.Fatalf("StepPlan over a recorded pending step: error %v, want refused", err)
	}
	if !strings.Contains(err.Error(), "step 2 has events or reviews") {
		t.Errorf("refusal %q does not name the first recorded step, 2", err)
	}
	cur, err := s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID, Section: core.SectionSteps})
	if err != nil {
		t.Fatal(err)
	}
	if len(cur.Steps) != 3 {
		t.Errorf("steps after refused plan = %d, want 3 unchanged", len(cur.Steps))
	}
}

// TestBlockAndAbandonWriteReasonToJournal checks that task_update writes the
// reason of a block or an abandon to the journal, as CONTRACT §1.5 requires.
func TestBlockAndAbandonWriteReasonToJournal(t *testing.T) {
	s, st := openLocal(t)
	ctx := agent("acme-api", "main")
	res, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	blocked := core.TaskBlocked
	if _, err := s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, Status: &blocked, Reason: "waiting on review"}); err != nil {
		t.Fatal(err)
	}
	abandoned := core.TaskAbandoned
	if _, err := s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, Status: &abandoned, Reason: "superseded"}); err != nil {
		t.Fatal(err)
	}
	events, _, err := store.ListEvents(context.Background(), st, store.EventFilter{TaskID: res.ID, Project: "acme-api", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("journal rows = %d, want 2", len(events))
	}
	// Newest first: the abandon, then the block.
	if events[0].Kind != string(core.EventDecision) || events[0].Text != "Abandoned: superseded" {
		t.Errorf("abandon row = %s %q", events[0].Kind, events[0].Text)
	}
	if events[1].Kind != string(core.EventBlocked) || events[1].Text != "waiting on review" {
		t.Errorf("block row = %s %q", events[1].Kind, events[1].Text)
	}
}

// TestStepSkipFromFailed checks that a failed step can be skipped, as
// CONTRACT §3 allows. Mutation testing showed that no test reached this edge
// of the skip rule.
func TestStepSkipFromFailed(t *testing.T) {
	s, _ := openLocal(t)
	ctx := agent("acme-api", "main")
	res, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g", Steps: []core.StepInput{{Title: "one"}}})
	if err != nil {
		t.Fatal(err)
	}
	one := core.NumberRef(1)
	if _, err := s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: one, Action: core.StepStart}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: one, Action: core.StepFail, Summary: "broke"}); err != nil {
		t.Fatal(err)
	}
	st, err := s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: one, Action: core.StepSkip, Summary: "not needed"})
	if err != nil {
		t.Fatalf("skip of a failed step: %v", err)
	}
	if st.Status != core.StepSkipped {
		t.Errorf("status after skip = %q, want skipped", st.Status)
	}
}

// TestTaskStartActiveSetsStartedAt checks that a task started active has its
// start time set at once, as task_start sets it.
func TestTaskStartActiveSetsStartedAt(t *testing.T) {
	s, _ := openLocal(t)
	ctx := agent("acme-api", "main")
	res, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID})
	if err != nil {
		t.Fatal(err)
	}
	if d.Task.StartedAt == nil {
		t.Errorf("active task has no started_at")
	}
}

// TestQueuedTaskStartedAtOnActivate checks that a task queued by task_start has
// no start time, and that the first move to active sets one. The row is read
// back through task_get.
func TestQueuedTaskStartedAtOnActivate(t *testing.T) {
	s, _ := openLocal(t)
	ctx := agent("acme-api", "main")
	res, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g", Queue: true})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID})
	if err != nil {
		t.Fatal(err)
	}
	if queued.Task.StartedAt != nil {
		t.Errorf("queued task has started_at %v before activation, want none", *queued.Task.StartedAt)
	}
	active := core.TaskActive
	if _, err := s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, Status: &active}); err != nil {
		t.Fatal(err)
	}
	d, err := s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID})
	if err != nil {
		t.Fatal(err)
	}
	if d.Task.StartedAt == nil {
		t.Errorf("queued task activated by task_update has no started_at")
	}
}

// TestReactivatedTaskKeepsStartedAt checks that a task blocked after its first
// activation, then reactivated, keeps the start time of that first activation.
// A start time is set only when the task has none. The sleep makes a fresh
// start time differ from the first, so a write of one would show.
func TestReactivatedTaskKeepsStartedAt(t *testing.T) {
	s, _ := openLocal(t)
	ctx := agent("acme-api", "main")
	res, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g", Queue: true})
	if err != nil {
		t.Fatal(err)
	}
	active := core.TaskActive
	if _, err := s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, Status: &active}); err != nil {
		t.Fatal(err)
	}
	d, err := s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID})
	if err != nil || d.Task.StartedAt == nil {
		t.Fatalf("first activation: started_at nil, err %v", err)
	}
	first := *d.Task.StartedAt

	time.Sleep(time.Millisecond)
	blocked := core.TaskBlocked
	if _, err := s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, Status: &blocked, Reason: "waiting"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, Status: &active}); err != nil {
		t.Fatal(err)
	}
	d, err = s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID})
	if err != nil {
		t.Fatal(err)
	}
	if d.Task.StartedAt == nil || !d.Task.StartedAt.Equal(first) {
		t.Errorf("started_at after reactivation = %v, want the first %v", d.Task.StartedAt, first)
	}
}

// TestTaskUpdateKeyFilesWithBlock checks one task_update that carries key files
// and a move to blocked. The files are replaced, and the blocked journal row is
// written, in that one update. The file replacement runs before the journal
// write, so a failure there must leave neither change behind. A refused write
// of the files is not reachable through a valid call, so the error branch
// itself is not run here.
func TestTaskUpdateKeyFilesWithBlock(t *testing.T) {
	s, st := openLocal(t)
	ctx := agent("acme-api", "main")
	res, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	files := []core.TaskFile{{Path: "internal/core/caller.go", Note: "the rule"}}
	blocked := core.TaskBlocked
	if _, err := s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, Status: &blocked, Reason: "waiting on review", KeyFiles: &files}); err != nil {
		t.Fatal(err)
	}
	d, err := s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID})
	if err != nil {
		t.Fatal(err)
	}
	if d.Task.Status != core.TaskBlocked {
		t.Errorf("status = %q, want blocked", d.Task.Status)
	}
	if len(d.KeyFiles) != 1 || d.KeyFiles[0].Path != "internal/core/caller.go" {
		t.Errorf("key files = %+v, want the one given", d.KeyFiles)
	}
	events, _, err := store.ListEvents(context.Background(), st, store.EventFilter{TaskID: res.ID, Project: "acme-api", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != string(core.EventBlocked) || events[0].Text != "waiting on review" {
		t.Errorf("journal = %+v, want one blocked row with the reason", events)
	}
}

// TestTaskUpdateCriteriaAndKeyFiles checks that one task_update writes both the
// criteria and the key files it is given. Each is replaced only when given, and
// writing one must not skip the other.
func TestTaskUpdateCriteriaAndKeyFiles(t *testing.T) {
	s, _ := openLocal(t)
	ctx := agent("acme-api", "main")
	res, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	criteria := []string{"tests pass"}
	files := []core.TaskFile{{Path: "internal/core/caller.go", Note: "the rule"}}
	if _, err := s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, Criteria: &criteria, KeyFiles: &files}); err != nil {
		t.Fatal(err)
	}
	d, err := s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Criteria) != 1 || d.Criteria[0].Criterion != "tests pass" {
		t.Errorf("criteria after update = %+v, want the one given", d.Criteria)
	}
	if len(d.KeyFiles) != 1 || d.KeyFiles[0].Path != "internal/core/caller.go" {
		t.Errorf("key files after update = %+v, want the one given", d.KeyFiles)
	}
}

// TestRatifiedDirectiveHasRatifiedAt checks that a directive read back after
// ratification carries its ratification time. The time is set on the row when
// it is ratified, and the read must show it.
func TestRatifiedDirectiveHasRatifiedAt(t *testing.T) {
	s, _ := openLocal(t)
	if _, err := s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "Check origins", Rule: "r"}); err != nil {
		t.Fatal(err)
	}
	ds, err := s.DirectiveList(agent("acme-api", "main"), core.DirectiveListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ds.Directives) != 1 || ds.Directives[0].RatifiedAt.IsZero() {
		t.Errorf("listed directives = %+v, want one with ratified_at set", ds.Directives)
	}
}
