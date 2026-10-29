package conformance

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
)

var eventCases = []Case{
	{"event/append_only_with_server_time", func(t T, s core.Service) {
		res := startTask(t, s, mainCtx(), "t", "one")
		before := time.Now().UTC().Add(-time.Second)
		texts := []string{"first", "second", "third"}
		for _, text := range texts {
			e := must(s.EventLog(as(projA, "hicks"), core.EventLogRequest{TaskID: res.ID, Kind: core.EventDecision, Text: text}))(t)
			if e.Agent != "hicks" || e.Project != projA || e.TaskID != res.ID {
				t.Errorf("event identity = agent %q project %q task %q", e.Agent, e.Project, e.TaskID)
			}
		}
		after := time.Now().UTC().Add(time.Second)
		list := must(s.EventList(mainCtx(), core.EventListRequest{TaskID: res.ID}))(t)
		var got []string
		for _, e := range list.Events {
			if e.Kind == core.EventDecision {
				got = append(got, e.Text)
			}
			if e.OccurredAt.Before(before) || e.OccurredAt.After(after) {
				t.Errorf("event %d occurred_at %v is not the server's time (between %v and %v)", e.ID, e.OccurredAt, before, after)
			}
			if e.OccurredAt.Location() != time.UTC {
				t.Errorf("event %d occurred_at %v is not UTC", e.ID, e.OccurredAt)
			}
		}
		if want := []string{"third", "second", "first"}; !slices.Equal(got, want) {
			t.Errorf("events newest first = %v, want %v", got, want)
		}
	}},
	{"event/kinds_agents_may_log", func(t T, s core.Service) {
		res := startTask(t, s, mainCtx(), "t")
		for _, k := range []core.EventKind{core.EventNote, core.EventBlocked, core.EventUnblocked, core.EventHandoff, core.EventDecision, core.EventCustom} {
			must(s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: k, Text: string(k)}))(t)
		}
		for _, k := range []core.EventKind{core.EventStarted, core.EventFinished, core.EventReview, core.EventClose, "gossip"} {
			_, err := s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: k, Text: "x"})
			wantKind(t, err, core.KindInvalid, "event_log kind "+string(k))
		}
		_, err := s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventNote})
		wantKind(t, err, core.KindInvalid, "event without text")
		_, err = s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventNote, Text: "x", Data: json.RawMessage(`[1,2]`)})
		wantKind(t, err, core.KindInvalid, "event data that is not an object")
		e := must(s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventCustom, Text: "x", Data: json.RawMessage(`{"word":"step-sync"}`)}))(t)
		var data map[string]string
		if err := json.Unmarshal(e.Data, &data); err != nil || data["word"] != "step-sync" {
			t.Errorf("event data = %s (%v)", e.Data, err)
		}
	}},
	{"event/list_pages", func(t T, s core.Service) {
		res := startTask(t, s, mainCtx(), "t")
		for i := 0; i < 25; i++ {
			must(s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventNote, Text: "n"}))(t)
		}
		seen := map[int64]bool{}
		cursor := ""
		pages := 0
		for {
			page := must(s.EventList(mainCtx(), core.EventListRequest{TaskID: res.ID, Limit: 10, Cursor: cursor}))(t)
			pages++
			for _, e := range page.Events {
				if seen[e.ID] {
					t.Errorf("event %d on two pages", e.ID)
				}
				seen[e.ID] = true
			}
			if page.Next == "" || pages > 10 {
				break
			}
			cursor = page.Next
		}
		if len(seen) < 25 || pages != (len(seen)+9)/10 {
			t.Errorf("paged %d events over %d pages", len(seen), pages)
		}
		def := must(s.EventList(mainCtx(), core.EventListRequest{TaskID: res.ID}))(t)
		if len(def.Events) != 20 {
			t.Errorf("default page = %d events, want 20", len(def.Events))
		}
	}},
	{"event/review_record_writes_event", func(t T, s core.Service) {
		res := startTask(t, s, mainCtx(), "t", "build", "review")
		rv := must(s.ReviewRecord(as(projA, "reviewer"), core.ReviewRecordRequest{
			TaskID: res.ID, Step: core.NumberRef(1), Verdict: core.VerdictNotApproved, Critical: 1, Warnings: 2, Summary: "missing test",
		}))(t)
		if rv.Reviewer != "reviewer" || rv.StepID != res.Steps[0].ID || rv.Critical != 1 {
			t.Errorf("review = %+v", rv)
		}
		_, err := s.ReviewRecord(mainCtx(), core.ReviewRecordRequest{TaskID: res.ID, Step: core.NumberRef(1), Verdict: "meh"})
		wantKind(t, err, core.KindInvalid, "unknown verdict")
		_, err = s.ReviewRecord(mainCtx(), core.ReviewRecordRequest{TaskID: res.ID, Step: core.NumberRef(1), Verdict: core.VerdictApproved, Critical: -1})
		wantKind(t, err, core.KindInvalid, "negative count")
		d := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID}))(t)
		if len(d.Reviews) != 1 {
			t.Errorf("task reviews = %+v", d.Reviews)
		}
		found := false
		for _, e := range d.Events {
			found = found || e.Kind == core.EventReview
		}
		if !found {
			t.Errorf("no review event in %+v", d.Events)
		}
	}},
}

var closeCases = []Case{
	{"close/whole_task", func(t T, s core.Service) {
		ctx := mainCtx()
		res := must(s.TaskStart(ctx, core.TaskStartRequest{
			Title: "Fix login redirect", Goal: "Login returns to the page the user came from.",
			Criteria: []string{"Redirect test passes", "No open redirect"},
			Steps:    []core.StepInput{{Title: "Write failing test"}, {Title: "Fix"}, {Title: "Review", Agent: "reviewer"}},
		}))(t)
		for n := 1; n <= 2; n++ {
			ref := core.NumberRef(n)
			must(s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: ref, Action: core.StepStart}))(t)
			must(s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: ref, Action: core.StepFinish, Summary: "ok"}))(t)
		}
		must(s.EventLog(ctx, core.EventLogRequest{TaskID: res.ID, Kind: core.EventDecision, Text: "Allow only same-origin paths."}))(t)
		must(s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(3), Action: core.StepStart}))(t)
		must(s.ReviewRecord(as(projA, "reviewer"), core.ReviewRecordRequest{TaskID: res.ID, Step: core.NumberRef(2), Verdict: core.VerdictApproved}))(t)
		l := must(s.LearningAdd(ctx, core.LearningAddRequest{TaskID: res.ID, Kind: core.LearningFinding, Title: "Check redirects", Body: "Every redirect needs an origin check.", Target: "reviewer"}))(t)
		if l.Status != core.LearningProposed || l.TaskID != res.ID {
			t.Errorf("learning = %+v", l)
		}

		// A bare close lists everything the default policy needs.
		_, err := s.TaskClose(ctx, core.TaskCloseRequest{ID: res.ID})
		wantKind(t, err, core.KindRefused, "bare close")
		missing := core.MissingOf(err)
		for _, key := range []string{core.MissingOutcome, core.MissingSummary, core.MissingCriteria, core.MissingLessons, core.MissingMistakes, core.MissingActiveSteps} {
			if !slices.Contains(missing, key) {
				t.Errorf("refused close missing = %v, want %q among them", missing, key)
			}
		}
		// Refusal changes nothing.
		if d := must(s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID}))(t); d.Task.Status != core.TaskActive {
			t.Errorf("status after refused close = %q", d.Task.Status)
		}

		must(s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(3), Action: core.StepFinish, Summary: "approved"}))(t)
		req := fullClose(res.ID, 2)
		req.Criteria[1].Result = core.CriterionPartial
		req.Mistakes = []core.Mistake{{Agent: "main", Mistake: "Edited before testing", Impact: core.ImpactLow, Prevention: "Test first"}}
		req.Deliverables = []core.TaskFile{{Path: "internal/auth/redirect.go"}}
		closed := must(s.TaskClose(ctx, req))(t)
		if closed.Status != core.TaskDone || closed.OutcomeSummary != req.Summary || closed.ClosedAt == nil {
			t.Errorf("closed task = %+v", closed)
		}
		d := must(s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID, Section: core.SectionClose}))(t)
		if len(d.Criteria) != 2 || d.Criteria[1].Result != core.CriterionPartial {
			t.Errorf("criteria after close = %+v", d.Criteria)
		}
		if len(d.Lessons) != 1 || len(d.Mistakes) != 1 || len(d.Deliverables) != 1 || d.Deliverables[0].Role != core.FileDeliverable {
			t.Errorf("close record = lessons %+v mistakes %+v deliverables %+v", d.Lessons, d.Mistakes, d.Deliverables)
		}
		if cur := must(s.TaskCurrent(ctx))(t); cur.Task != nil {
			t.Errorf("TaskCurrent after close = %s", cur.Task.ID)
		}
		_, err = s.TaskClose(ctx, req)
		wantKind(t, err, core.KindRefused, "closing twice")
		_, err = s.StepAdd(ctx, core.StepAddRequest{TaskID: res.ID, Title: "late"})
		wantKind(t, err, core.KindRefused, "step on a closed task")
	}},
	{"close/empty_lists_versus_absent", func(t T, s core.Service) {
		res := startTask(t, s, mainCtx(), "t")
		req := fullClose(res.ID, 0)
		req.Lessons = []core.Lesson{}
		_, err := s.TaskClose(mainCtx(), req)
		wantKind(t, err, core.KindRefused, "no lessons and no reason")
		if m := core.MissingOf(err); !slices.Equal(m, []string{core.MissingLessons}) {
			t.Errorf("missing = %v, want [lessons]", m)
		}
		req.Mistakes = nil
		req.NoLessonsReason = "Routine dependency bump."
		_, err = s.TaskClose(mainCtx(), req)
		if m := core.MissingOf(err); !slices.Equal(m, []string{core.MissingMistakes}) {
			t.Errorf("missing = %v, want [mistakes]: an absent list is not an empty one", m)
		}
		req.Mistakes = []core.Mistake{}
		closed := must(s.TaskClose(mainCtx(), req))(t)
		if closed.Status != core.TaskDone {
			t.Errorf("status = %q", closed.Status)
		}
		d := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionClose}))(t)
		if d.NoLessonsReason != req.NoLessonsReason {
			t.Errorf("no_lessons_reason = %q", d.NoLessonsReason)
		}
	}},
	{"close/failed_outcome_and_validation", func(t T, s core.Service) {
		res := must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "t", Goal: "g", Criteria: []string{"c"}}))(t)
		req := fullClose(res.ID, 1)
		req.Outcome = core.TaskAbandoned
		_, err := s.TaskClose(mainCtx(), req)
		wantKind(t, err, core.KindInvalid, "outcome abandoned through task_close")
		req.Outcome = core.TaskFailed
		req.Criteria = []core.CriterionOutcome{{Ord: 2, Result: core.CriterionMet}}
		_, err = s.TaskClose(mainCtx(), req)
		wantKind(t, err, core.KindInvalid, "result for a criterion that does not exist")
		req.Criteria = []core.CriterionOutcome{{Ord: 1, Result: core.CriterionUnmet}}
		req.Mistakes = []core.Mistake{{Agent: "main", Mistake: "m", Impact: "huge"}}
		_, err = s.TaskClose(mainCtx(), req)
		wantKind(t, err, core.KindInvalid, "unknown mistake impact")
		req.Mistakes = []core.Mistake{}
		closed := must(s.TaskClose(mainCtx(), req))(t)
		if closed.Status != core.TaskFailed {
			t.Errorf("status = %q, want failed", closed.Status)
		}
	}},
	{"close/policy_is_project_settings", func(t T, s core.Service) {
		lax := core.ProjectSettings{} // every requirement off
		must(s.ProjectSettingsSet(op(), core.ProjectSettingsRequest{Project: projA, Settings: ptr(lax)}))(t)
		res := startTask(t, s, mainCtx(), "t", "one")
		must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(1), Action: core.StepStart}))(t)
		closed := must(s.TaskClose(mainCtx(), core.TaskCloseRequest{ID: res.ID, Outcome: core.TaskDone}))(t)
		if closed.Status != core.TaskDone {
			t.Errorf("status under a lax policy = %q", closed.Status)
		}
		// Another project keeps the default policy.
		other := startTask(t, s, as(projB, "main"), "t")
		_, err := s.TaskClose(as(projB, "main"), core.TaskCloseRequest{ID: other.ID, Outcome: core.TaskDone})
		wantKind(t, err, core.KindRefused, "bare close under the default policy")
	}},
	{"close/refused_close_keeps_record", func(t T, s core.Service) {
		// CONTRACT §2: a refused close changes nothing. This close carries every
		// part of a close (criteria results, lessons, mistakes, deliverables) and
		// is refused for want of a summary. Read back, none of them is written.
		res := must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "t", Goal: "g", Criteria: []string{"a", "b"}}))(t)
		req := fullClose(res.ID, 2)
		req.Summary = ""
		req.Mistakes = []core.Mistake{{Agent: "main", Mistake: "Edited before testing", Impact: core.ImpactLow}}
		req.Deliverables = []core.TaskFile{{Path: "internal/auth/redirect.go"}}
		_, err := s.TaskClose(mainCtx(), req)
		wantKind(t, err, core.KindRefused, "close without a summary")
		if m := core.MissingOf(err); !slices.Equal(m, []string{core.MissingSummary}) {
			t.Errorf("missing = %v, want [summary]", m)
		}

		d := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionClose}))(t)
		for _, c := range d.Criteria {
			if c.Result != "" {
				t.Errorf("criterion %d has result %q after a refused close", c.Ord, c.Result)
			}
		}
		if len(d.Lessons) != 0 || len(d.Mistakes) != 0 || len(d.Deliverables) != 0 {
			t.Errorf("record after a refused close: lessons %+v mistakes %+v deliverables %+v", d.Lessons, d.Mistakes, d.Deliverables)
		}
		if d.NoLessonsReason != "" || d.Task.Status != core.TaskActive || d.Task.ClosedAt != nil {
			t.Errorf("after a refused close: no_lessons_reason %q status %q closed_at %v", d.NoLessonsReason, d.Task.Status, d.Task.ClosedAt)
		}
	}},
	{"close/writes_one_close_event", func(t T, s core.Service) {
		// CONTRACT §2: an accepted close writes a close event. It is written
		// once, by the caller, and its text carries the outcome.
		res := startTask(t, s, mainCtx(), "t")
		must(s.TaskClose(as(projA, "closer"), fullClose(res.ID, 0)))(t)
		d := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID}))(t)
		var closes []core.Event
		for _, e := range d.Events {
			if e.Kind == core.EventClose {
				closes = append(closes, e)
			}
		}
		if len(closes) != 1 {
			t.Fatalf("close events = %+v, want exactly one", closes)
		}
		if closes[0].Agent != "closer" || closes[0].Text != "Closed as done" {
			t.Errorf("close event agent %q text %q, want closer and \"Closed as done\"", closes[0].Agent, closes[0].Text)
		}
	}},
	{"close/settings_must_be_given", func(t T, s core.Service) {
		// CONTRACT §8: the call replaces the whole settings object, so a call
		// that gives none is invalid. Read as a zero value, it would switch every
		// close requirement off and parallel tasks off as well.
		parallel := core.DefaultSettings()
		parallel.ParallelTasks = true
		must(s.ProjectSettingsSet(op(), core.ProjectSettingsRequest{Project: projA, Settings: &parallel}))(t)
		_, err := s.ProjectSettingsSet(op(), core.ProjectSettingsRequest{Project: projA})
		wantKind(t, err, core.KindInvalid, "project_settings_set without settings")

		// The settings stand: two tasks may be active at once, and the default
		// close policy still applies.
		one := must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "one", Goal: "g"}))(t)
		must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "two", Goal: "g"}))(t)
		_, err = s.TaskClose(mainCtx(), core.TaskCloseRequest{ID: one.ID, Outcome: core.TaskDone})
		wantKind(t, err, core.KindRefused, "bare close under the default policy after a refused settings call")
	}},
}
