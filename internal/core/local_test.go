package core_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/conformance"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// part3 names the part of issue #9 that brings in the calls the deferred cases
// still need: the operator calls and task_close with its close policy. Parts 1
// and 2 are live; a case deferred now waits only on part 3.
const part3 = "issue #9 part 3 (operator calls, task_close and close policy)"

// deferred lists the cases that part 2 does not yet pass. Each is reported as
// skipped with its reason, and is neither a pass nor a fail. RunWith fails if
// a name here is not in the suite, and if a name here passes.
var deferred = map[string]string{
	"identity/no_project_refused":           "needs task_close, which is " + part3,
	"identity/write_without_agent_refused":  "needs task_close, which is " + part3,
	"identity/operator_calls_refuse_agents": "needs the operator calls, which are " + part3,
	"task/parallel_when_project_allows":     "needs project_settings_set, which is " + part3,
	"close/whole_task":                      "needs task_close, which is " + part3,
	"close/empty_lists_versus_absent":       "needs task_close, which is " + part3,
	"close/failed_outcome_and_validation":   "needs task_close, which is " + part3,
	"close/policy_is_project_settings":      "needs task_close and project_settings_set, which are " + part3,
	"learning/operator_decides":             "needs learning_decide, which is " + part3,
	"directive/scopes_apply":                "needs directive_ratify and directive_retire, which are " + part3,
	"directive/from_learning":               "needs directive_ratify from a learning, which is " + part3,
	"recall/across_projects":                "needs task_close and directive_ratify, which are " + part3,
	"search/finds_each_kind":                "needs directive_ratify, which is " + part3,
	"search/hides_retired_directives":       "needs directive_ratify and directive_retire, which are " + part3,
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
// writes, read back through event_list.
func TestStepStartAndFinishWriteEvents(t *testing.T) {
	s, _ := openLocal(t)
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
	two := core.StepRef{Number: 2}
	if _, err := s.EventLog(ctx, core.EventLogRequest{TaskID: res.ID, Step: &two, Kind: core.EventNote, Text: "about two"}); err != nil {
		t.Fatal(err)
	}
	three := core.StepRef{Number: 3}
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
