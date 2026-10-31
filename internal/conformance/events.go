package conformance

import (
	"encoding/json"
	"slices"
	"strings"
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
		list := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionEvents}))(t)
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
		for _, k := range []core.EventKind{core.EventStarted, core.EventFinished, core.EventClose, "gossip"} {
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
			page := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionEvents, Limit: 10, Cursor: cursor}))(t)
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
		def := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionEvents}))(t)
		if len(def.Events) != 20 {
			t.Errorf("default page = %d events, want 20", len(def.Events))
		}
	}},
	{"event/step_refs_must_name_a_step", func(t T, s core.Service) {
		// CONTRACT §1.4: a step ref names one step. {} names none, number 0
		// names none (it is valid only as step_add's after), and an id of 0 with
		// a number is two faults at once. Each of the two journal calls that
		// take a ref refuses all three, and writes nothing.
		res := startTask(t, s, mainCtx(), "t", "one")
		refs := []struct {
			what string
			ref  core.StepRef
		}{
			{"{}", core.StepRef{}},
			{"number 0", core.NumberRef(0)},
			{"number -1", core.NumberRef(-1)},
			{"id -1", core.StepRef{ID: ptr(int64(-1))}},
			{"id 0 with number 1", core.StepRef{ID: ptr(int64(0)), Number: ptr(1)}},
		}
		for _, r := range refs {
			ref := r.ref
			_, err := s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Step: &ref, Kind: core.EventNote, Text: "refused"})
			wantKind(t, err, core.KindInvalid, "event_log with step "+r.what)
			_, err = s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionEvents, Step: &ref})
			wantKind(t, err, core.KindInvalid, "task_get events with step "+r.what)
		}
		d := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionEvents}))(t)
		for _, e := range d.Events {
			if e.Text == "refused" {
				t.Errorf("a refused event_log was written: %+v", e)
			}
		}
	}},
	{"event/pending_step_takes_review_and_event", func(t T, s core.Service) {
		// CONTRACT §9 items 33 and 39: event_log accepts a review of a step in any
		// status, and the step here is still pending.
		res := startTask(t, s, mainCtx(), "t", "one")
		step := core.NumberRef(1)
		must(s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Step: &step, Kind: core.EventNote, Text: "Before the work starts."}))(t)
		rv := must(s.EventLog(as(projA, "reviewer"), core.EventLogRequest{TaskID: res.ID, Step: &step, Kind: core.EventReview, Text: "Plan read.", Data: reviewData("approved", 0, 0)}))(t)
		if rv.StepID == nil || *rv.StepID != res.Steps[0].ID {
			t.Errorf("review of a pending step names step %v, want %d", rv.StepID, res.Steps[0].ID)
		}
	}},
	{"event/review_data_is_recorded", func(t T, s core.Service) {
		// CONTRACT §4, §9 item 39: a review event keeps its verdict and counts in
		// data, names its agent as the reviewer, and is read back through task_get.
		res := startTask(t, s, mainCtx(), "t", "build")
		step := core.NumberRef(1)
		rv := must(s.EventLog(as(projA, "reviewer"), core.EventLogRequest{TaskID: res.ID, Step: &step, Kind: core.EventReview, Text: "Missing test.", Data: reviewData("not-approved", 1, 2)}))(t)
		if rv.Kind != core.EventReview || rv.Agent != "reviewer" || rv.StepID == nil || *rv.StepID != res.Steps[0].ID {
			t.Errorf("review event = %+v", rv)
		}
		var got map[string]any
		if err := json.Unmarshal(rv.Data, &got); err != nil || got["verdict"] != "not-approved" || got["critical"] != float64(1) || got["warnings"] != float64(2) {
			t.Errorf("review data = %s (%v)", rv.Data, err)
		}
		d := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionEvents}))(t)
		found := false
		for _, e := range d.Events {
			found = found || (e.Kind == core.EventReview && e.ID == rv.ID)
		}
		if !found {
			t.Errorf("review %d is not in the journal: %+v", rv.ID, d.Events)
		}
	}},
	{"event/review_data_missing_field_refused", func(t T, s core.Service) {
		// CONTRACT §4: a review needs data with a verdict and two counts. A missing
		// or malformed field is invalid, the message names the fix, and no row is
		// written.
		res := startTask(t, s, mainCtx(), "t")
		for _, c := range []struct{ what, data string }{
			{"no data", ""},
			{"no verdict", `{"critical": 0, "warnings": 0}`},
			{"no critical", `{"verdict": "approved", "warnings": 0}`},
			{"no warnings", `{"verdict": "approved", "critical": 0}`},
			{"a count as text", `{"verdict": "approved", "critical": "0", "warnings": 0}`},
			{"a count as a fraction", `{"verdict": "approved", "critical": 0.5, "warnings": 0}`},
			{"data as an array", `[1, 2, 3]`},
		} {
			var data json.RawMessage
			if c.data != "" {
				data = json.RawMessage(c.data)
			}
			_, err := s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventReview, Text: "x", Data: data})
			wantKind(t, err, core.KindInvalid, "review with "+c.what)
			if err != nil && !strings.Contains(err.Error(), `"not-approved"`) {
				t.Errorf("review with %s: the message %q does not name the fix", c.what, err)
			}
		}
		assertNoReviewEvents(t, s, res.ID)
	}},
	{"event/review_data_key_case_refused", func(t T, s core.Service) {
		// CONTRACT §4: the keys are spelled exactly as verdict, critical and
		// warnings. A key in another case is not read as one of them, so it is
		// invalid, and the message names the key.
		res := startTask(t, s, mainCtx(), "t")
		for _, data := range []string{
			`{"Verdict": "approved", "critical": 0, "warnings": 0}`,
			`{"verdict": "approved", "CRITICAL": 0, "warnings": 0}`,
			`{"verdict": "approved", "critical": 0, "Warnings": 0}`,
		} {
			_, err := s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventReview, Text: "x", Data: json.RawMessage(data)})
			wantKind(t, err, core.KindInvalid, "review data "+data)
		}
		assertNoReviewEvents(t, s, res.ID)
	}},
	{"event/review_data_extra_key_refused", func(t T, s core.Service) {
		// CONTRACT §4: review data holds exactly the three keys. An extra key is
		// invalid and names itself, so a summary put in data is not dropped silently.
		res := startTask(t, s, mainCtx(), "t")
		_, err := s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventReview, Text: "x",
			Data: json.RawMessage(`{"verdict": "approved", "critical": 0, "warnings": 0, "summary": "ok"}`)})
		wantKind(t, err, core.KindInvalid, "review data with an extra key")
		if err != nil && !strings.Contains(err.Error(), `"summary"`) {
			t.Errorf("the message %q does not name the extra key", err)
		}
		assertNoReviewEvents(t, s, res.ID)
	}},
	{"event/review_data_null_refused", func(t T, s core.Service) {
		// CONTRACT §4: null is no value. Null data, and a null in any of the three
		// fields, is invalid, as a missing field is.
		res := startTask(t, s, mainCtx(), "t")
		for _, data := range []string{
			`null`,
			`{"verdict": null, "critical": 0, "warnings": 0}`,
			`{"verdict": "approved", "critical": null, "warnings": 0}`,
			`{"verdict": "approved", "critical": 0, "warnings": null}`,
		} {
			_, err := s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventReview, Text: "x", Data: json.RawMessage(data)})
			wantKind(t, err, core.KindInvalid, "review data "+data)
		}
		assertNoReviewEvents(t, s, res.ID)
	}},
	{"event/review_data_repeated_key_refused", func(t T, s core.Service) {
		// CONTRACT §4: a key given twice is invalid. encoding/json keeps the last
		// of two, so the repeat is checked on its own.
		res := startTask(t, s, mainCtx(), "t")
		_, err := s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventReview, Text: "x",
			Data: json.RawMessage(`{"verdict": "not-approved", "verdict": "approved", "critical": 1, "warnings": 0}`)})
		wantKind(t, err, core.KindInvalid, "review data with a repeated verdict")
		assertNoReviewEvents(t, s, res.ID)
	}},
	{"event/review_wrong_verdict_refused", func(t T, s core.Service) {
		// CONTRACT §4: the verdict is approved or not-approved, exactly.
		res := startTask(t, s, mainCtx(), "t")
		for _, v := range []string{"meh", "Approved", "", "passed"} {
			_, err := s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventReview, Text: "x", Data: reviewData(v, 0, 0)})
			wantKind(t, err, core.KindInvalid, "review verdict "+v)
		}
		assertNoReviewEvents(t, s, res.ID)
	}},
	{"event/review_negative_count_refused", func(t T, s core.Service) {
		// CONTRACT §4: each count is 0 or more. A negative critical or warnings
		// count is invalid, and writes nothing.
		res := startTask(t, s, mainCtx(), "t")
		_, err := s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventReview, Text: "x", Data: reviewData("approved", -1, 0)})
		wantKind(t, err, core.KindInvalid, "negative critical")
		_, err = s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventReview, Text: "x", Data: reviewData("approved", 0, -1)})
		wantKind(t, err, core.KindInvalid, "negative warnings")
		assertNoReviewEvents(t, s, res.ID)
	}},
	{"event/review_on_terminal_task_refused", func(t T, s core.Service) {
		// CONTRACT §1.5: every write to a terminal task is refused. A review with
		// valid data is a write, so it is refused on an abandoned task and writes
		// no row. The data check runs first, so only valid data reaches this rule.
		res := startTask(t, s, mainCtx(), "t", "one")
		abandoned := core.TaskAbandoned
		must(s.TaskUpdate(mainCtx(), core.TaskUpdateRequest{ID: res.ID, Status: &abandoned, Reason: "superseded"}))(t)
		step := core.NumberRef(1)
		_, err := s.EventLog(as(projA, "reviewer"), core.EventLogRequest{TaskID: res.ID, Step: &step, Kind: core.EventReview, Text: "Late review.", Data: reviewData("approved", 0, 0)})
		wantKind(t, err, core.KindRefused, "valid review on an abandoned task")
		assertNoReviewEvents(t, s, res.ID)
	}},
	{"event/review_on_each_step_status", func(t T, s core.Service) {
		// CONTRACT §9 item 39: a review is accepted on a step in any of its five
		// statuses. Step 1 is reviewed while pending, active and done, step 2
		// once failed, and step 3 once skipped. Each review names its step.
		res := startTask(t, s, mainCtx(), "t", "one", "two", "three")
		reviewer := as(projA, "reviewer")
		var got []int64
		review := func(ref core.StepRef) {
			e := must(s.EventLog(reviewer, core.EventLogRequest{TaskID: res.ID, Step: &ref, Kind: core.EventReview, Text: "Reviewed.", Data: reviewData("approved", 0, 0)}))(t)
			if e.StepID == nil {
				t.Fatalf("review event has no step")
			}
			got = append(got, *e.StepID)
		}
		step1, step2, step3 := core.NumberRef(1), core.NumberRef(2), core.NumberRef(3)
		review(step1)
		must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: step1, Action: core.StepStart}))(t)
		review(step1)
		must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: step1, Action: core.StepFinish, Summary: "Built it."}))(t)
		review(step1)
		must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: step2, Action: core.StepStart}))(t)
		must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: step2, Action: core.StepFail, Summary: "Broke."}))(t)
		review(step2)
		must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: step3, Action: core.StepSkip, Summary: "Not needed."}))(t)
		review(step3)

		// The reviews were taken while the steps stood in the statuses the case
		// names, so the steps are read back to confirm the setup.
		steps := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionSteps}))(t).Steps
		var statuses []core.StepStatus
		for _, st := range steps {
			statuses = append(statuses, st.Status)
		}
		if want := []core.StepStatus{core.StepDone, core.StepFailed, core.StepSkipped}; !slices.Equal(statuses, want) {
			t.Errorf("steps read back as %v, want %v", statuses, want)
		}

		want := []int64{res.Steps[0].ID, res.Steps[0].ID, res.Steps[0].ID, res.Steps[1].ID, res.Steps[2].ID}
		if !slices.Equal(got, want) {
			t.Errorf("reviews name steps %v, want %v", got, want)
		}
		d := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionEvents, Limit: core.MaxLimit}))(t)
		reviews := 0
		for _, e := range d.Events {
			if e.Kind == core.EventReview {
				reviews++
			}
		}
		if reviews != 5 {
			t.Errorf("review events in the journal = %d, want 5", reviews)
		}
	}},
	{"event/empty_page_has_events_list", func(t T, s core.Service) {
		// CONTRACT §2: a task_get of section events always carries an events list.
		// A page with no events is an empty list, not an absent one, so a client
		// can tell no events from no section. Step 1 has no events here.
		res := startTask(t, s, mainCtx(), "t", "one")
		step := core.NumberRef(1)
		d := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionEvents, Step: &step}))(t)
		if d.Events == nil || len(d.Events) != 0 {
			t.Errorf("events of a step with none = %v, want an empty list", d.Events)
		}
		if d.Next != "" {
			t.Errorf("next = %q on the last page, want none", d.Next)
		}
	}},
	{"task_get/paging_fields_need_events", func(t T, s core.Service) {
		// CONTRACT §2: limit, cursor and step apply to section events only. With
		// any other section, and with no section, each one is invalid.
		res := startTask(t, s, mainCtx(), "t", "one")
		step := core.NumberRef(1)
		for _, section := range []core.TaskSection{core.SectionAll, core.SectionBrief, core.SectionSteps, core.SectionClose} {
			_, err := s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: section, Limit: 5})
			wantKind(t, err, core.KindInvalid, "task_get section "+string(section)+" with limit")
			_, err = s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: section, Cursor: "x"})
			wantKind(t, err, core.KindInvalid, "task_get section "+string(section)+" with cursor")
			_, err = s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: section, Step: &step})
			wantKind(t, err, core.KindInvalid, "task_get section "+string(section)+" with step")
		}
	}},
	{"task_get/bad_paging_refused", func(t T, s core.Service) {
		// CONTRACT §1.3, §2: for section events a limit is 0 to 100, a cursor must
		// decode, and a step ref must name exactly one step. Each fault is invalid.
		res := startTask(t, s, mainCtx(), "t", "one")
		for _, c := range []struct {
			what string
			req  core.TaskGetRequest
		}{
			{"a negative limit", core.TaskGetRequest{Limit: -1}},
			{"a limit above the maximum", core.TaskGetRequest{Limit: core.MaxLimit + 1}},
			{"a cursor that does not decode", core.TaskGetRequest{Cursor: "!!!"}},
			{"an empty step ref", core.TaskGetRequest{Step: &core.StepRef{}}},
		} {
			req := c.req
			req.ID, req.Section = res.ID, core.SectionEvents
			_, err := s.TaskGet(mainCtx(), req)
			wantKind(t, err, core.KindInvalid, "task_get events with "+c.what)
		}
		if _, err := s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionEvents, Limit: core.MaxLimit}); err != nil {
			t.Errorf("task_get events with the maximum limit: %v", err)
		}
	}},
}

// policySwitchCase is the case for one close-policy switch (CONTRACT §9 item 27).
// Only that switch is on. A task with a criterion and one active step is closed
// with nothing given: the close is refused with exactly the item the switch
// guards in missing, and it passes once give supplies that item. sw picks the
// switch from a ClosePolicy, and give fills the close request.
func policySwitchCase(name, key string, sw func(p *core.ClosePolicy) *bool, give func(t T, s core.Service, id string, req *core.TaskCloseRequest)) Case {
	return Case{Name: name, Run: func(t T, s core.Service) {
		var p core.ProjectSettings
		*sw(&p.ClosePolicy) = true
		must(s.ProjectSettingsSet(op(), core.ProjectSettingsRequest{Project: projA, Settings: p.AsInput()}))(t)
		res := must(s.TaskStart(mainCtx(), core.TaskStartRequest{
			Title: "t", Goal: "g", Criteria: []string{"c"}, Steps: []core.StepInput{{Title: "one"}},
		}))(t)
		must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: res.ID, Step: core.NumberRef(1), Action: core.StepStart}))(t)

		req := core.TaskCloseRequest{ID: res.ID, Outcome: core.TaskDone}
		_, err := s.TaskClose(mainCtx(), req)
		wantKind(t, err, core.KindRefused, "close with only "+key+" on, given nothing")
		if m := core.MissingOf(err); !slices.Equal(m, []string{key}) {
			t.Errorf("missing = %v, want [%s]", m, key)
		}
		give(t, s, res.ID, &req)
		closed := must(s.TaskClose(mainCtx(), req))(t)
		if closed.Status != core.TaskDone {
			t.Errorf("status = %q, want done once %s is given", closed.Status, key)
		}
	}}
}

var closeCases = []Case{
	{"close/lessons_kept_with_no_lessons_reason", func(t T, s core.Service) {
		// CONTRACT §2, §9 item 26: a close with lessons and a no_lessons_reason
		// stores the reason as given. It is neither refused nor dropped.
		res := startTask(t, s, mainCtx(), "t")
		req := fullClose(res.ID, 0)
		req.NoLessonsReason = "The lesson above covers it."
		closed := must(s.TaskClose(mainCtx(), req))(t)
		if closed.Status != core.TaskDone {
			t.Errorf("status = %q, want done", closed.Status)
		}
		d := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: res.ID, Section: core.SectionClose}))(t)
		if d.NoLessonsReason != req.NoLessonsReason || len(d.Lessons) != 1 {
			t.Errorf("no_lessons_reason %q with %d lessons, want the reason as given and the one lesson", d.NoLessonsReason, len(d.Lessons))
		}
	}},
	policySwitchCase("close/policy_switch_summary", core.MissingSummary,
		func(p *core.ClosePolicy) *bool { return &p.RequireSummary },
		func(t T, s core.Service, id string, req *core.TaskCloseRequest) {
			req.Summary = "Done."
		}),
	policySwitchCase("close/policy_switch_criteria", core.MissingCriteria,
		func(p *core.ClosePolicy) *bool { return &p.RequireCriteriaResults },
		func(t T, s core.Service, id string, req *core.TaskCloseRequest) {
			req.Criteria = []core.CriterionOutcome{{Ord: 1, Result: core.CriterionMet, Evidence: "ok"}}
		}),
	policySwitchCase("close/policy_switch_lessons", core.MissingLessons,
		func(p *core.ClosePolicy) *bool { return &p.RequireLessons },
		func(t T, s core.Service, id string, req *core.TaskCloseRequest) {
			req.Lessons = []core.Lesson{{Kind: core.LessonReuse, Text: "Reuse the parser.", Why: "It works."}}
		}),
	policySwitchCase("close/policy_switch_mistakes", core.MissingMistakes,
		func(p *core.ClosePolicy) *bool { return &p.RequireMistakes },
		func(t T, s core.Service, id string, req *core.TaskCloseRequest) {
			req.Mistakes = []core.Mistake{}
		}),
	policySwitchCase("close/policy_switch_active_steps", core.MissingActiveSteps,
		func(p *core.ClosePolicy) *bool { return &p.RequireNoActiveSteps },
		func(t T, s core.Service, id string, req *core.TaskCloseRequest) {
			must(s.StepUpdate(mainCtx(), core.StepUpdateRequest{TaskID: id, Step: core.NumberRef(1), Action: core.StepFinish, Summary: "ok"}))(t)
		}),
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
		must(s.EventLog(as(projA, "reviewer"), core.EventLogRequest{TaskID: res.ID, Step: ptr(core.NumberRef(2)), Kind: core.EventReview, Text: "Step 2 looked at by the reviewer.", Data: reviewData("approved", 0, 0)}))(t)
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
		must(s.ProjectSettingsSet(op(), core.ProjectSettingsRequest{Project: projA, Settings: lax.AsInput()}))(t)
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
		must(s.ProjectSettingsSet(op(), core.ProjectSettingsRequest{Project: projA, Settings: parallel.AsInput()}))(t)
		_, err := s.ProjectSettingsSet(op(), core.ProjectSettingsRequest{Project: projA})
		wantKind(t, err, core.KindInvalid, "project_settings_set without settings")

		// The settings stand: two tasks may be active at once, and the default
		// close policy still applies.
		one := must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "one", Goal: "g"}))(t)
		must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "two", Goal: "g"}))(t)
		_, err = s.TaskClose(mainCtx(), core.TaskCloseRequest{ID: one.ID, Outcome: core.TaskDone})
		wantKind(t, err, core.KindRefused, "bare close under the default policy after a refused settings call")
	}},
	{"close/settings_must_be_whole", func(t T, s core.Service) {
		// CONTRACT §8, §9 item 23: an object missing any one switch is invalid,
		// as a missing object is. Read as off, a missing close switch would
		// quietly lift one requirement, so each partial object is refused.
		whole := core.DefaultSettings()
		whole.ParallelTasks = true
		must(s.ProjectSettingsSet(op(), core.ProjectSettingsRequest{Project: projA, Settings: whole.AsInput()}))(t)

		noParallel := whole.AsInput()
		noParallel.ParallelTasks = nil
		_, err := s.ProjectSettingsSet(op(), core.ProjectSettingsRequest{Project: projA, Settings: noParallel})
		wantKind(t, err, core.KindInvalid, "settings without parallel_tasks")

		noMistakes := whole.AsInput()
		noMistakes.ClosePolicy.RequireMistakes = nil
		_, err = s.ProjectSettingsSet(op(), core.ProjectSettingsRequest{Project: projA, Settings: noMistakes})
		wantKind(t, err, core.KindInvalid, "settings without close_policy.require_mistakes")

		// The stored settings stand: two tasks may be active, and the mistakes
		// requirement still applies to a close that gives none.
		one := must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "one", Goal: "g"}))(t)
		must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "two", Goal: "g"}))(t)
		_, err = s.TaskClose(mainCtx(), core.TaskCloseRequest{ID: one.ID, Outcome: core.TaskDone})
		wantKind(t, err, core.KindRefused, "bare close after refused partial settings")
	}},
}

// reviewData is the data of a review event with the given verdict and counts.
func reviewData(verdict string, critical, warnings int) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"verdict": verdict, "critical": critical, "warnings": warnings})
	return b
}

// assertNoReviewEvents checks that the journal of task id has no review event.
// The refused reviews of a case must not have written one.
func assertNoReviewEvents(t T, s core.Service, id string) {
	t.Helper()
	d := must(s.TaskGet(mainCtx(), core.TaskGetRequest{ID: id, Section: core.SectionEvents}))(t)
	for _, e := range d.Events {
		if e.Kind == core.EventReview {
			t.Errorf("a refused review was written: %+v", e)
		}
	}
}
