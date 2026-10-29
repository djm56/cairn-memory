package core_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/conformance"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// part2 and part3 name the parts of issue #9 that bring in the calls a deferred
// case needs. Part 1 is tasks and steps.
const (
	part2 = "issue #9 part 2 (events, learnings, search)"
	part3 = "issue #9 part 3 (operator calls and close policy)"
)

// deferred lists the cases part 1 does not yet pass. Each is reported as
// skipped with its reason, and is neither a pass nor a fail. RunWith fails if
// a name here is not in the suite.
var deferred = map[string]string{
	"identity/no_project_refused":           "needs every agent call; " + part2 + " and " + part3,
	"identity/write_without_agent_refused":  "needs every write call; " + part2 + " and " + part3,
	"identity/operator_calls_refuse_agents": "needs learning_add and the operator calls; " + part2 + " and " + part3,
	"task/abandoned_is_terminal":            "needs event_log; " + part2,
	"task/parallel_when_project_allows":     "needs project_settings_set; " + part3,
	"step/transitions_and_attempts":         "needs event_list; " + part2,
	"event/append_only_with_server_time":    part2,
	"event/kinds_agents_may_log":            part2,
	"event/list_pages":                      part2,
	"event/review_record_writes_event":      part2,
	"close/whole_task":                      part3,
	"close/empty_lists_versus_absent":       part3,
	"close/failed_outcome_and_validation":   part3,
	"close/policy_is_project_settings":      part3,
	"learning/add_is_proposed_only":         part2,
	"learning/operator_decides":             part2,
	"directive/scopes_apply":                part2,
	"directive/from_learning":               part2 + " (ratify from a learning is part 3)",
	"note/put_and_get":                      part2,
	"recall/across_projects":                part2,
	"search/finds_each_kind":                part2,
}

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
// writes. event_list reads them in part 2, so this reads the store directly.
func TestStepStartAndFinishWriteEvents(t *testing.T) {
	s, st := openLocal(t)
	ctx := agent("acme-api", "main")
	res, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g", Steps: []core.StepInput{{Title: "one"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: core.StepRef{Number: 1}, Action: core.StepStart}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: core.StepRef{Number: 1}, Action: core.StepFinish, Summary: "ok"}); err != nil {
		t.Fatal(err)
	}
	events, _, err := store.ListEvents(context.Background(), st, store.EventFilter{TaskID: res.ID, Project: "acme-api", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for i := len(events) - 1; i >= 0; i-- {
		kinds = append(kinds, events[i].Kind)
	}
	if len(kinds) != 2 || kinds[0] != string(core.EventStarted) || kinds[1] != string(core.EventFinished) {
		t.Errorf("journal kinds = %v, want [started finished]", kinds)
	}
}

// TestStepPlanRefusesPendingStepWithRecords checks that step_plan does not
// detach a journal: a pending step with an event cannot be removed.
func TestStepPlanRefusesPendingStepWithRecords(t *testing.T) {
	s, st := openLocal(t)
	ctx := agent("acme-api", "main")
	res, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g", Steps: []core.StepInput{{Title: "one"}, {Title: "two"}}})
	if err != nil {
		t.Fatal(err)
	}
	stepID := res.Steps[1].ID
	// event_log is part 2, so the event is written through the store.
	if _, err := store.InsertEvent(context.Background(), st, store.EventRow{
		TaskID: res.ID, Project: "acme-api", StepID: &stepID, Agent: "main",
		Kind: string(core.EventNote), Text: "about two", OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	_, err = s.StepPlan(ctx, core.StepPlanRequest{TaskID: res.ID, Steps: []core.StepInput{{Title: "new"}}})
	if core.KindOf(err) != core.KindRefused {
		t.Fatalf("StepPlan over a recorded pending step: error %v, want refused", err)
	}
	cur, err := s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID, Section: core.SectionSteps})
	if err != nil {
		t.Fatal(err)
	}
	if len(cur.Steps) != 2 {
		t.Errorf("steps after refused plan = %d, want 2 unchanged", len(cur.Steps))
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
