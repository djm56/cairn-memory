package conformance

import (
	"regexp"
	"slices"

	"github.com/djm56/cairn-memory/internal/core"
)

var taskID = regexp.MustCompile(`^task-\d{8}-\d{2,}$`)

var taskCases = []Case{
	{"task/current_none", func(t T, s core.Service) {
		cur := must(s.TaskCurrent(mainCtx()))(t)
		if cur.Task != nil {
			t.Errorf("TaskCurrent on an empty project returned task %s", cur.Task.ID)
		}
	}},
	{"task/start_returns_id_and_becomes_current", func(t T, s core.Service) {
		res := must(s.TaskStart(mainCtx(), core.TaskStartRequest{
			Title:    "Add rate limit",
			Goal:     "Requests over 100/min get 429.",
			Context:  "Abuse seen last week.",
			Criteria: []string{"429 after 100 requests", "Limit configurable"},
			KeyFiles: []core.TaskFile{{Path: "internal/api/router.go"}},
			Steps:    []core.StepInput{{Title: "Write test", Agent: "main"}, {Title: "Implement"}},
		}))(t)
		if !taskID.MatchString(res.ID) {
			t.Errorf("task id %q does not match task-YYYYMMDD-NN", res.ID)
		}
		if res.Status != core.TaskActive {
			t.Errorf("new task status = %q, want active", res.Status)
		}
		if got := stepTitles(t, res.Steps); !slices.Equal(got, []string{"Write test", "Implement"}) {
			t.Errorf("steps = %v", got)
		}
		cur := must(s.TaskCurrent(mainCtx()))(t)
		if cur.Task == nil || cur.Task.ID != res.ID {
			t.Fatalf("TaskCurrent = %+v, want task %s", cur.Task, res.ID)
		}
		if cur.Task.Project != projA || cur.Task.Priority != core.PriorityNormal {
			t.Errorf("task project %q priority %q, want %q normal", cur.Task.Project, cur.Task.Priority, projA)
		}
		if len(cur.Criteria) != 2 || cur.Criteria[0].Ord != 1 || cur.Criteria[1].Criterion != "Limit configurable" {
			t.Errorf("criteria = %+v", cur.Criteria)
		}
		if len(cur.Steps) != 2 || cur.Steps[0].Status != core.StepPending || cur.Steps[0].Agent != "main" {
			t.Errorf("steps = %+v", cur.Steps)
		}
		brief := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionBrief}))(t)
		if len(brief.KeyFiles) != 1 || brief.KeyFiles[0].Role != core.FileKey {
			t.Errorf("key files = %+v, want one with role key", brief.KeyFiles)
		}
		if brief.Task.Context != "Abuse seen last week." {
			t.Errorf("context = %q", brief.Task.Context)
		}
	}},
	{"task/ids_distinct", func(t T, s core.Service) {
		seen := map[string]bool{}
		for i := 0; i < 5; i++ {
			res := must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "t", Goal: "g", Queue: true}))(t)
			if seen[res.ID] {
				t.Errorf("task id %s allocated twice", res.ID)
			}
			seen[res.ID] = true
		}
		// The sequence is global, not per project (decision D4).
		res := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "t", Goal: "g"}))(t)
		if seen[res.ID] {
			t.Errorf("task id %s reused in another project", res.ID)
		}
	}},
	{"task/start_requires_title_and_goal", func(t T, s core.Service) {
		_, err := s.TaskStart(mainCtx(), core.TaskStartRequest{Goal: "g"})
		wantKind(t, err, core.KindInvalid, "task without title")
		_, err = s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "t"})
		wantKind(t, err, core.KindInvalid, "task without goal")
		_, err = s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "t", Goal: "g", Priority: "whenever"})
		wantKind(t, err, core.KindInvalid, "unknown priority")
	}},
	{"task/one_active_per_project", func(t T, s core.Service) {
		first := startTask(t, s, mainCtx(), "first")
		_, err := s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "second", Goal: "g"})
		wantKind(t, err, core.KindRefused, "second active task")
		queued := must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "queued", Goal: "g", Queue: true}))(t)
		if queued.Status != core.TaskOpen {
			t.Errorf("queued task status = %q, want open", queued.Status)
		}
		_, err = s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{ID: queued.ID, Status: ptr(core.TaskActive)})
		wantKind(t, err, core.KindRefused, "activating a second task")
		// Another project is independent.
		startTask(t, s, as(projB, "main"), "other project")
		// Blocking the first frees the slot.
		must(s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{ID: first.ID, Status: ptr(core.TaskBlocked), Reason: "waiting on review"}))(t)
		must(s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{ID: queued.ID, Status: ptr(core.TaskActive)}))(t)
		cur := must(s.TaskCurrent(mainCtx()))(t)
		if cur.Task == nil || cur.Task.ID != queued.ID {
			t.Errorf("TaskCurrent = %+v, want %s", cur.Task, queued.ID)
		}
	}},
	{"task/parallel_when_project_allows", func(t T, s core.Service) {
		settings := core.DefaultSettings()
		settings.ParallelTasks = true
		must(s.ProjectSettingsSet(op(), core.ProjectSettingsRequest{Project: projA, Settings: settings}))(t)
		startTask(t, s, mainCtx(), "first")
		second := startTask(t, s, mainCtx(), "second")
		cur := must(s.TaskCurrent(mainCtx()))(t)
		if cur.Task == nil || cur.Task.ID != second.ID || cur.OtherActive != 1 {
			t.Errorf("TaskCurrent = task %+v, other_active %d; want %s and 1", cur.Task, cur.OtherActive, second.ID)
		}
	}},
	{"task/update_fields_and_reasons", func(t T, s core.Service) {
		res := startTask(t, s, mainCtx(), "t")
		got := must(s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{
			ID: res.ID, Priority: ptr(core.PriorityHigh), Owner: ptr("main"), NextAction: ptr("write the test"),
		}))(t)
		if got.Priority != core.PriorityHigh || got.Owner != "main" || got.NextAction != "write the test" {
			t.Errorf("updated task = %+v", got)
		}
		_, err := s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{ID: res.ID, Status: ptr(core.TaskBlocked)})
		wantKind(t, err, core.KindInvalid, "blocked without a reason")
		_, err = s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{ID: res.ID, Status: ptr(core.TaskDone)})
		wantKind(t, err, core.KindInvalid, "done through task_update")
		_, err = s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{ID: "task-19990101-01", NextAction: ptr("x")})
		wantKind(t, err, core.KindNotFound, "unknown task")
		// A task in another project is not found, not shown.
		_, err = s.TaskGet(as(projB, "main"), core.TaskGetRequest{ID: res.ID})
		wantKind(t, err, core.KindNotFound, "task from another project")
	}},
	{"task/brief_frozen_once_work_starts", func(t T, s core.Service) {
		res := startTask(t, s, mainCtx(), "t", "one")
		must(s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{ID: res.ID, Goal: ptr("A sharper goal."), Criteria: ptr([]string{"c1"})}))(t)
		must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(1), Action: core.StepStart}))(t)
		_, err := s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{ID: res.ID, Goal: ptr("Moving the goalposts.")})
		wantKind(t, err, core.KindRefused, "brief edit after a step started")
		// Status and next action stay editable.
		must(s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{ID: res.ID, NextAction: ptr("finish step 1")}))(t)
		d := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionBrief}))(t)
		if d.Task.Goal != "A sharper goal." || len(d.Criteria) != 1 {
			t.Errorf("brief = goal %q, criteria %+v", d.Task.Goal, d.Criteria)
		}
	}},
	{"task/abandoned_is_terminal", func(t T, s core.Service) {
		res := startTask(t, s, mainCtx(), "t")
		got := must(s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{ID: res.ID, Status: ptr(core.TaskAbandoned), Reason: "superseded"}))(t)
		if got.Status != core.TaskAbandoned || got.ClosedAt == nil {
			t.Errorf("abandoned task = status %q closed_at %v", got.Status, got.ClosedAt)
		}
		_, err := s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{ID: res.ID, Status: ptr(core.TaskActive)})
		wantKind(t, err, core.KindRefused, "reopening an abandoned task")
		_, err = s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventNote, Text: "late"})
		wantKind(t, err, core.KindRefused, "event on an abandoned task")
		if cur := must(s.TaskCurrent(mainCtx()))(t); cur.Task != nil {
			t.Errorf("TaskCurrent after abandon = %s", cur.Task.ID)
		}
	}},
	{"task/update_never_sets_open", func(t T, s core.Service) {
		// CONTRACT §1.5 (ruling 1a): task_update does not move a task into open.
		res := startTask(t, s, mainCtx(), "t")
		_, err := s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{ID: res.ID, Status: ptr(core.TaskOpen)})
		wantKind(t, err, core.KindRefused, "task_update to open on an active task")
		got := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionBrief}))(t)
		if got.Task.Status != core.TaskActive {
			t.Errorf("status after the refused move = %q, want active", got.Task.Status)
		}
		queued := must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "queued", Goal: "g", Queue: true}))(t)
		if queued.Status != core.TaskOpen {
			t.Errorf("queued task status = %q, want open", queued.Status)
		}
	}},
	{"task/list_filters_and_pages", func(t T, s core.Service) {
		active := startTask(t, s, mainCtx(), "active")
		for i := 0; i < 3; i++ {
			must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "queued", Goal: "g", Queue: true}))(t)
		}
		startTask(t, s, as(projB, "main"), "elsewhere")
		all := must(s.TaskList(mainCtx(), core.TaskListRequest{}))(t)
		if len(all.Tasks) != 4 {
			t.Errorf("task_list returned %d tasks, want the project's 4", len(all.Tasks))
		}
		act := must(s.TaskList(mainCtx(), core.TaskListRequest{Status: []core.TaskStatus{core.TaskActive}}))(t)
		if len(act.Tasks) != 1 || act.Tasks[0].ID != active.ID {
			t.Errorf("active tasks = %+v", act.Tasks)
		}
		page1 := must(s.TaskList(mainCtx(), core.TaskListRequest{Limit: 3}))(t)
		if len(page1.Tasks) != 3 || page1.Next == "" {
			t.Fatalf("page 1 = %d tasks, next %q", len(page1.Tasks), page1.Next)
		}
		page2 := must(s.TaskList(mainCtx(), core.TaskListRequest{Limit: 3, Cursor: page1.Next}))(t)
		if len(page2.Tasks) != 1 || page2.Next != "" {
			t.Errorf("page 2 = %d tasks, next %q", len(page2.Tasks), page2.Next)
		}
		_, err := s.TaskList(mainCtx(), core.TaskListRequest{Limit: core.MaxLimit + 1})
		wantKind(t, err, core.KindInvalid, "limit over the maximum")
	}},
}

var stepCases = []Case{
	{"step/insert_without_renumbering", func(t T, s core.Service) {
		res := startTask(t, s, mainCtx(), "t", "one", "two", "three")
		ids := []int64{res.Steps[0].ID, res.Steps[1].ID, res.Steps[2].ID}
		added := must(s.StepAdd(mainCtx(), core.StepAddRequest{TaskID: res.ID, After: ptr(core.NumberRef(1)), Title: "one-and-a-half"}))(t)
		if added.Number != 2 {
			t.Errorf("inserted step number = %d, want 2", added.Number)
		}
		first := must(s.StepAdd(mainCtx(), core.StepAddRequest{TaskID: res.ID, After: ptr(core.NumberRef(0)), Title: "zero"}))(t)
		last := must(s.StepAdd(mainCtx(), core.StepAddRequest{TaskID: res.ID, Title: "four"}))(t)
		d := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionSteps}))(t)
		want := []string{"zero", "one", "one-and-a-half", "two", "three", "four"}
		if got := stepTitles(t, d.Steps); !slices.Equal(got, want) {
			t.Fatalf("steps = %v, want %v", got, want)
		}
		// Ids never change; only display numbers do.
		gotIDs := []int64{d.Steps[1].ID, d.Steps[3].ID, d.Steps[4].ID}
		if !slices.Equal(gotIDs, ids) || d.Steps[0].ID != first.ID || d.Steps[5].ID != last.ID {
			t.Errorf("step ids changed: %+v", d.Steps)
		}
		// Many inserts at the same place keep their order.
		for i := 0; i < 30; i++ {
			must(s.StepAdd(mainCtx(), core.StepAddRequest{TaskID: res.ID, After: ptr(core.IDRef(first.ID)), Title: "squeezed"}))(t)
		}
		d = must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionSteps}))(t)
		titles := stepTitles(t, d.Steps)
		if len(titles) != 36 || titles[0] != "zero" || titles[31] != "one" || titles[35] != "four" {
			t.Errorf("after 30 inserts: %v", titles)
		}
	}},
	{"step/ref_by_id_or_number", func(t T, s core.Service) {
		res := startTask(t, s, mainCtx(), "t", "one", "two")
		byNum := must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(2), Action: core.StepStart}))(t)
		if byNum.ID != res.Steps[1].ID {
			t.Errorf("step 2 by number = id %d, want %d", byNum.ID, res.Steps[1].ID)
		}
		byID := must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.IDRef(res.Steps[1].ID), Action: core.StepFinish, Summary: "done"}))(t)
		if byID.Status != core.StepDone {
			t.Errorf("step by id status = %q", byID.Status)
		}
		_, err := s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.StepRef{ID: ptr(res.Steps[0].ID), Number: ptr(1)}, Action: core.StepStart})
		wantKind(t, err, core.KindInvalid, "step ref with both id and number")
		_, err = s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.StepRef{}, Action: core.StepStart})
		wantKind(t, err, core.KindInvalid, "empty step ref")
		_, err = s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(9), Action: core.StepStart})
		wantKind(t, err, core.KindNotFound, "step 9 of 2")
	}},
	{"step/empty_ref_is_invalid", func(t T, s core.Service) {
		// CONTRACT §1.4 (ruling 2a): {} names no step, so it is invalid. Only
		// {"number": 0} means before the first step, and only as step_add's after.
		res := startTask(t, s, mainCtx(), "t", "one")
		_, err := s.StepAdd(mainCtx(), core.StepAddRequest{TaskID: res.ID, After: ptr(core.StepRef{}), Title: "empty"})
		wantKind(t, err, core.KindInvalid, "step_add after an empty ref")
		_, err = s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(0), Action: core.StepStart})
		wantKind(t, err, core.KindInvalid, "step_update on number 0")
		kept := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionSteps}))(t)
		if titles := stepTitles(t, kept.Steps); !slices.Equal(titles, []string{"one"}) {
			t.Errorf("steps after the refused adds = %v, want [one]", titles)
		}
	}},
	{"step/transitions_and_attempts", func(t T, s core.Service) {
		res := startTask(t, s, mainCtx(), "t", "one", "two")
		one := core.NumberRef(1)
		upd := func(a core.StepAction, summary string) (*core.Step, error) {
			return s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: one, Action: a, Summary: summary})
		}
		_, err := upd(core.StepFinish, "x")
		wantKind(t, err, core.KindRefused, "finish a pending step")
		st := must(upd(core.StepStart, ""))(t)
		if st.Status != core.StepActive || st.Attempts != 1 || st.StartedAt == nil {
			t.Errorf("started step = %+v", st)
		}
		_, err = upd(core.StepFail, "")
		wantKind(t, err, core.KindInvalid, "fail without a summary")
		must(upd(core.StepFail, "test still red"))(t)
		st = must(upd(core.StepStart, ""))(t)
		if st.Attempts != 2 {
			t.Errorf("attempts after retry = %d, want 2", st.Attempts)
		}
		st = must(upd(core.StepFinish, "green"))(t)
		if st.Status != core.StepDone || st.Summary != "green" || st.EndedAt == nil {
			t.Errorf("finished step = %+v", st)
		}
		_, err = upd(core.StepStart, "")
		wantKind(t, err, core.KindRefused, "restart a done step")
		skipped := must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(2), Action: core.StepSkip, Summary: "not needed", Agent: ptr("hicks")}))(t)
		if skipped.Status != core.StepSkipped || skipped.Agent != "hicks" {
			t.Errorf("skipped step = %+v", skipped)
		}
		// Each change wrote its event.
		evs := must(s.EventList(mainCtx(), core.EventListRequest{TaskID: res.ID, Step: &one}))(t)
		var kinds []core.EventKind
		for _, e := range evs.Events {
			kinds = append(kinds, e.Kind)
		}
		if !slices.Contains(kinds, core.EventStarted) || !slices.Contains(kinds, core.EventFinished) {
			t.Errorf("step 1 events = %v, want started and finished among them", kinds)
		}
	}},
	{"step/start_needs_active_task", func(t T, s core.Service) {
		res := must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "t", Goal: "g", Queue: true, Steps: []core.StepInput{{Title: "one"}}}))(t)
		_, err := s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(1), Action: core.StepStart})
		wantKind(t, err, core.KindRefused, "start a step of an open task")
	}},
	{"step/plan_keeps_started_steps", func(t T, s core.Service) {
		res := startTask(t, s, mainCtx(), "t", "one", "two", "three")
		must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(1), Action: core.StepStart}))(t)
		must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(1), Action: core.StepFinish, Summary: "ok"}))(t)
		must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(2), Action: core.StepStart}))(t)
		got := must(s.StepPlan(mainCtx(), core.StepPlanRequest{TaskID: res.ID, Steps: []core.StepInput{{Title: "new-a"}, {Title: "new-b"}}}))(t)
		want := []string{"one", "two", "new-a", "new-b"}
		if titles := stepTitles(t, got.Steps); !slices.Equal(titles, want) {
			t.Errorf("after replan = %v, want %v", titles, want)
		}
		if got.Steps[0].ID != res.Steps[0].ID || got.Steps[1].Status != core.StepActive {
			t.Errorf("started steps not kept: %+v", got.Steps[:2])
		}
	}},
	{"step/plan_refuses_recorded_pending_step", func(t T, s core.Service) {
		// CONTRACT §3 (ruling 3a): a pending step with an event or a review
		// cannot be removed, so step_plan is refused. The way out is to skip it.
		res := startTask(t, s, mainCtx(), "t", "one", "two")
		two := core.NumberRef(2)
		must(s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Step: &two, Kind: core.EventNote, Text: "about two"}))(t)
		_, err := s.StepPlan(mainCtx(), core.StepPlanRequest{TaskID: res.ID, Steps: []core.StepInput{{Title: "new"}}})
		wantKind(t, err, core.KindRefused, "step_plan over a pending step with an event")
		kept := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionSteps}))(t)
		if titles := stepTitles(t, kept.Steps); !slices.Equal(titles, []string{"one", "two"}) {
			t.Errorf("steps after the refused plan = %v, want unchanged", titles)
		}
		must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: two, Action: core.StepSkip, Summary: "not needed"}))(t)
		got := must(s.StepPlan(mainCtx(), core.StepPlanRequest{TaskID: res.ID, Steps: []core.StepInput{{Title: "new"}}}))(t)
		// The pending step "one" is removed; the skipped step "two" is kept.
		if titles := stepTitles(t, got.Steps); !slices.Equal(titles, []string{"two", "new"}) {
			t.Errorf("steps after skip and replan = %v, want [two new]", titles)
		}
	}},
}
