package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// completeClose is a close that meets the default policy for a task with two
// criteria, both met. The checks below change one thing at a time.
func completeClose(id string) core.TaskCloseRequest {
	return core.TaskCloseRequest{
		ID:       id,
		Outcome:  core.TaskDone,
		Summary:  "Done.",
		Criteria: []core.CriterionOutcome{{Ord: 1, Result: core.CriterionMet}, {Ord: 2, Result: core.CriterionMet}},
		Lessons:  []core.Lesson{{Kind: core.LessonReuse, Text: "Reuse it.", Why: "It works."}},
		Mistakes: []core.Mistake{},
	}
}

// TestCloseFromOpenTaskRefused checks that a queued task cannot close: CONTRACT
// §1.5 draws task_close only from active and blocked. The request is complete,
// so the open refusal is the only thing that stops the close. Removing that
// refusal lets the close through and changes the status.
func TestCloseFromOpenTaskRefused(t *testing.T) {
	s, _ := openLocal(t)
	ctx := mainCtx()
	res := must(s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g", Queue: true, Criteria: []string{"a", "b"}}))(t)
	_, err := s.TaskClose(ctx, completeClose(res.ID))
	if core.KindOf(err) != core.KindRefused {
		t.Fatalf("close of an open task: error %v, want refused", err)
	}
	d := must(s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID, Section: core.SectionBrief}))(t)
	if d.Task.Status != core.TaskOpen {
		t.Errorf("status after refused close = %q, want open", d.Task.Status)
	}
}

// TestCloseShapeRefusals checks each request-shape check of task_close on its
// own. Each row changes one field of a complete close, so the row's fault is the
// one that refuses it. The first row is the complete close, which must pass:
// otherwise every row would fail for some other reason and prove nothing.
func TestCloseShapeRefusals(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*core.TaskCloseRequest)
		want   core.Kind // empty: the close must succeed
	}{
		{"complete close", func(*core.TaskCloseRequest) {}, ""},
		{"criterion given twice", func(r *core.TaskCloseRequest) {
			r.Criteria = append(r.Criteria, core.CriterionOutcome{Ord: 1, Result: core.CriterionUnmet})
		}, core.KindInvalid},
		{"criterion result unknown", func(r *core.TaskCloseRequest) {
			r.Criteria[0].Result = "gossip"
		}, core.KindInvalid},
		{"criterion result empty", func(r *core.TaskCloseRequest) {
			r.Criteria[0].Result = ""
		}, core.KindInvalid},
		{"lesson kind unknown", func(r *core.TaskCloseRequest) {
			r.Lessons[0].Kind = "rumour"
		}, core.KindInvalid},
		{"lesson text blank", func(r *core.TaskCloseRequest) {
			r.Lessons[0].Text = "  "
		}, core.KindInvalid},
		{"mistake without an agent", func(r *core.TaskCloseRequest) {
			r.Mistakes = []core.Mistake{{Mistake: "Edited first.", Impact: core.ImpactLow}}
		}, core.KindInvalid},
		{"mistake text blank", func(r *core.TaskCloseRequest) {
			r.Mistakes = []core.Mistake{{Agent: "main", Mistake: "", Impact: core.ImpactLow}}
		}, core.KindInvalid},
		{"deliverable without a path", func(r *core.TaskCloseRequest) {
			r.Deliverables = []core.TaskFile{{Path: " "}}
		}, core.KindInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := openLocal(t)
			ctx := mainCtx()
			task := must(s.TaskStart(ctx, core.TaskStartRequest{
				Title: "t", Goal: "g", Criteria: []string{"a", "b"},
			}))(t)
			req := completeClose(task.ID)
			c.mutate(&req)
			_, err := s.TaskClose(ctx, req)
			if c.want == "" {
				if err != nil {
					t.Fatalf("complete close: unexpected error %v", err)
				}
				return
			}
			if core.KindOf(err) != c.want {
				t.Errorf("error %v, want kind %s", err, c.want)
			}
		})
	}
}

// TestStoredSettingsReadTheSameInTheReadAndTheRules checks step 6 review W1: a stored settings
// object that is '{}', or that leaves switches out, reads its missing switches as the defaults,
// both in project_list and in the close rules. A switch the object sets to false stays off in
// both. The row is written first, with the object under test, so the first call does not
// replace it with the defaults.
func TestStoredSettingsReadTheSameInTheReadAndTheRules(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		// summaryRequired is what both the read and the close rule must say of the summary switch.
		summaryRequired bool
	}{
		{"the column default", "{}", true},
		{"parallel tasks only", `{"parallel_tasks":false}`, true},
		{"summary switch stored off", `{"close_policy":{"require_summary":false}}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, st := openLocal(t)
			at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			if err := store.EnsureProject(context.Background(), st, store.ProjectRow{
				Name: "acme-api", CreatedAt: at, LastSeenAt: at, Settings: c.raw,
			}); err != nil {
				t.Fatal(err)
			}
			ctx := agent("acme-api", "main")
			res := must(s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g"}))(t)

			list := must(s.ProjectList(op(), core.ProjectListRequest{Project: "acme-api"}))(t)
			if shown := list.Projects[0].Settings.ClosePolicy.RequireSummary; shown != c.summaryRequired {
				t.Errorf("project_list shows require_summary %v, want %v", shown, c.summaryRequired)
			}

			// No summary and no criteria: the summary switch alone decides whether this close is refused.
			close := completeClose(res.ID)
			close.Summary = ""
			close.Criteria = nil
			_, err := s.TaskClose(ctx, close)
			refused := core.KindOf(err) == core.KindRefused
			if refused != c.summaryRequired {
				t.Errorf("close without a summary: refused %v (error %v), want refused %v", refused, err, c.summaryRequired)
			}
		})
	}
}
