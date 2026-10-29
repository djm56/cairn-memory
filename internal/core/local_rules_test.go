package core_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// agentWrite is one agent call that writes, with the request shape checked
// elsewhere. The identity cases of the conformance suite cover every agent call
// through their own table, which needs task_close (part 3). These cover the
// same rules for every call but task_close, so they hold now.
type agentWrite struct {
	name string
	do   func(ctx context.Context, s *core.Local) error
}

// agentCallsNoClose returns one call of each agent method except task_close.
// Each call is plausible, so the only thing wrong with it is the caller.
func agentCallsNoClose() []agentWrite {
	const id = "task-20261029-01"
	return []agentWrite{
		{"TaskCurrent", func(ctx context.Context, s *core.Local) error { _, err := s.TaskCurrent(ctx); return err }},
		{"TaskStart", func(ctx context.Context, s *core.Local) error {
			_, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g"})
			return err
		}},
		{"TaskGet", func(ctx context.Context, s *core.Local) error {
			_, err := s.TaskGet(ctx, core.TaskGetRequest{ID: id})
			return err
		}},
		{"TaskList", func(ctx context.Context, s *core.Local) error {
			_, err := s.TaskList(ctx, core.TaskListRequest{})
			return err
		}},
		{"TaskUpdate", func(ctx context.Context, s *core.Local) error {
			_, err := s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: id, Title: strPtr("t")})
			return err
		}},
		{"StepPlan", func(ctx context.Context, s *core.Local) error {
			_, err := s.StepPlan(ctx, core.StepPlanRequest{TaskID: id, Steps: []core.StepInput{{Title: "s"}}})
			return err
		}},
		{"StepAdd", func(ctx context.Context, s *core.Local) error {
			_, err := s.StepAdd(ctx, core.StepAddRequest{TaskID: id, Title: "s"})
			return err
		}},
		{"StepUpdate", func(ctx context.Context, s *core.Local) error {
			_, err := s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: id, Step: core.NumberRef(1), Action: core.StepStart})
			return err
		}},
		{"EventLog", func(ctx context.Context, s *core.Local) error {
			_, err := s.EventLog(ctx, core.EventLogRequest{TaskID: id, Kind: core.EventNote, Text: "x"})
			return err
		}},
		{"EventList", func(ctx context.Context, s *core.Local) error {
			_, err := s.EventList(ctx, core.EventListRequest{TaskID: id})
			return err
		}},
		{"ReviewRecord", func(ctx context.Context, s *core.Local) error {
			_, err := s.ReviewRecord(ctx, core.ReviewRecordRequest{TaskID: id, Step: core.NumberRef(1), Verdict: core.VerdictApproved})
			return err
		}},
		{"LearningAdd", func(ctx context.Context, s *core.Local) error {
			_, err := s.LearningAdd(ctx, core.LearningAddRequest{Kind: core.LearningFinding, Title: "t", Body: "b"})
			return err
		}},
		{"LearningList", func(ctx context.Context, s *core.Local) error {
			_, err := s.LearningList(ctx, core.LearningListRequest{})
			return err
		}},
		{"DirectiveList", func(ctx context.Context, s *core.Local) error {
			_, err := s.DirectiveList(ctx, core.DirectiveListRequest{})
			return err
		}},
		{"Recall", func(ctx context.Context, s *core.Local) error {
			_, err := s.Recall(ctx, core.RecallRequest{Goal: "goal"})
			return err
		}},
		{"Search", func(ctx context.Context, s *core.Local) error {
			_, err := s.Search(ctx, core.SearchRequest{Query: "q"})
			return err
		}},
		{"NotePut", func(ctx context.Context, s *core.Local) error {
			_, err := s.NotePut(ctx, core.NotePutRequest{Title: "t", Body: "b"})
			return err
		}},
		{"NoteGet", func(ctx context.Context, s *core.Local) error {
			_, err := s.NoteGet(ctx, core.NoteGetRequest{ID: 1})
			return err
		}},
	}
}

func strPtr(s string) *string { return &s }

// writeCalls names the agent calls that write, for the agent-less check.
var writeCalls = map[string]bool{
	"TaskStart": true, "TaskUpdate": true, "StepPlan": true, "StepAdd": true,
	"StepUpdate": true, "EventLog": true, "ReviewRecord": true, "LearningAdd": true,
	"NotePut": true,
}

// TestNoProjectAndNoAgentRefused checks every agent call without a project, and
// every write without an agent. Both are invalid before any row is read, and
// the project error names the fix.
func TestNoProjectAndNoAgentRefused(t *testing.T) {
	s, _ := openLocal(t)
	noProject := core.WithCaller(context.Background(), core.Caller{Agent: "main", Role: core.RoleAgent})
	for _, c := range agentCallsNoClose() {
		err := c.do(noProject, s)
		if core.KindOf(err) != core.KindInvalid {
			t.Errorf("%s without a project: error %v, want invalid", c.name, err)
		} else if !strings.Contains(err.Error(), "cairn init") {
			t.Errorf("%s without a project: %q does not name cairn init", c.name, err)
		}
		if err := c.do(context.Background(), s); core.KindOf(err) != core.KindInvalid {
			t.Errorf("%s without any caller: error %v, want invalid", c.name, err)
		}
		if !writeCalls[c.name] {
			continue
		}
		if err := c.do(agent(projA, ""), s); core.KindOf(err) != core.KindInvalid {
			t.Errorf("%s without an agent: error %v, want invalid", c.name, err)
		}
	}
}

// TestTerminalTaskRefusesEveryWrite checks the rule of CONTRACT §1.5: once a
// task is abandoned, every write to it is refused, and the reads still answer.
// The steps are made first, so a step write meets a real step and the refusal
// is the rule that applies.
func TestTerminalTaskRefusesEveryWrite(t *testing.T) {
	s, _ := openLocal(t)
	ctx := mainCtx()
	res := must(s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g", Steps: []core.StepInput{{Title: "one"}, {Title: "two"}}}))(t)
	abandoned := core.TaskAbandoned
	must(s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, Status: &abandoned, Reason: "superseded"}))(t)

	one := core.NumberRef(1)
	active := core.TaskActive
	attempts := map[string]func() error{
		"TaskUpdate status": func() error {
			_, err := s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, Status: &active})
			return err
		},
		"TaskUpdate title": func() error {
			_, err := s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, Title: strPtr("new")})
			return err
		},
		"StepPlan": func() error {
			_, err := s.StepPlan(ctx, core.StepPlanRequest{TaskID: res.ID, Steps: []core.StepInput{{Title: "s"}}})
			return err
		},
		"StepAdd": func() error {
			_, err := s.StepAdd(ctx, core.StepAddRequest{TaskID: res.ID, Title: "late"})
			return err
		},
		"StepUpdate start": func() error {
			_, err := s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: one, Action: core.StepStart})
			return err
		},
		"StepUpdate skip": func() error {
			_, err := s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: one, Action: core.StepSkip, Summary: "no"})
			return err
		},
		"EventLog": func() error {
			_, err := s.EventLog(ctx, core.EventLogRequest{TaskID: res.ID, Kind: core.EventNote, Text: "late"})
			return err
		},
		"ReviewRecord": func() error {
			_, err := s.ReviewRecord(ctx, core.ReviewRecordRequest{TaskID: res.ID, Step: one, Verdict: core.VerdictApproved})
			return err
		},
	}
	for name, do := range attempts {
		if err := do(); core.KindOf(err) != core.KindRefused {
			t.Errorf("%s on an abandoned task: error %v, want refused", name, err)
		}
	}

	// Nothing changed: the steps are as planned, and the reads still answer.
	d := must(s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID}))(t)
	if d.Task.Status != core.TaskAbandoned || len(d.Steps) != 2 || d.Steps[0].Status != core.StepPending {
		t.Errorf("abandoned task after refused writes = %s with %d steps", d.Task.Status, len(d.Steps))
	}
}

// TestParallelTasksTaskCurrent checks task_current when the project allows
// parallel tasks, which is set through the store here (project_settings_set is
// part 3). Several active tasks are allowed, task_current returns the one
// started last, and other_active counts the rest. A blocked task does not count.
func TestParallelTasksTaskCurrent(t *testing.T) {
	s, st := openLocal(t)
	ctx := mainCtx()
	// The first call of a project creates its row, and setParallel edits it.
	must(s.TaskCurrent(ctx))(t)
	setParallel(t, st, projA, true)

	must(s.TaskStart(ctx, core.TaskStartRequest{Title: "a", Goal: "g"}))(t)
	b := must(s.TaskStart(ctx, core.TaskStartRequest{Title: "b", Goal: "g"}))(t)
	c := must(s.TaskStart(ctx, core.TaskStartRequest{Title: "c", Goal: "g"}))(t)

	cur := must(s.TaskCurrent(ctx))(t)
	if cur.Task == nil || cur.Task.ID != c.ID || cur.OtherActive != 2 {
		t.Fatalf("TaskCurrent = %+v, other_active %d; want %s, other_active 2", cur.Task, cur.OtherActive, c.ID)
	}

	// Blocking the newest hands the answer to the one started before it, and
	// the blocked task is no longer counted.
	blocked := core.TaskBlocked
	must(s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: c.ID, Status: &blocked, Reason: "waiting"}))(t)
	cur = must(s.TaskCurrent(ctx))(t)
	if cur.Task == nil || cur.Task.ID != b.ID || cur.OtherActive != 1 {
		t.Errorf("TaskCurrent after blocking %s = %+v, other_active %d; want %s, other_active 1", c.ID, cur.Task, cur.OtherActive, b.ID)
	}

	// With parallel tasks off, a second active task is refused again.
	setParallel(t, st, projA, false)
	_, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "d", Goal: "g"})
	if core.KindOf(err) != core.KindRefused {
		t.Errorf("second active task with parallel off: error %v, want refused", err)
	}
}

// setParallel writes the parallel_tasks setting of project through the store,
// keeping the default close policy.
func setParallel(t *testing.T, st *store.Store, project string, on bool) {
	t.Helper()
	ctx := context.Background()
	settings := core.DefaultSettings()
	settings.ParallelTasks = on
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	row, err := store.GetProject(ctx, st, project)
	if err != nil {
		t.Fatalf("get project %s: %v", project, err)
	}
	row.Settings = string(raw)
	if err := st.WithTx(ctx, func(q store.Querier) error {
		return store.UpdateProject(ctx, q, row)
	}); err != nil {
		t.Fatalf("set parallel on %s: %v", project, err)
	}
}
