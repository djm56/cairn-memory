package conformance

import (
	"fmt"
	"slices"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
)

// directiveCapCases hold the M2 directive rules (CONTRACT §5, §8, decision D16):
// the cap of 12, the ranking, and review_by on ratify. Expiry is not here. A
// directive expires when review_by has passed, and the suite cannot move the
// server's clock, so the core tests check expiry with a clock seam
// (internal/core/local_directives_test.go). The HTTP runner therefore does not
// exercise expiry; a case here can only check that a directive not yet due is
// listed and counted as not expired.
var directiveCapCases = []Case{
	{"directive/cap_twelve_of_fifteen", func(t T, s core.Service) {
		// CONTRACT §5: at most 12 come back, the newest kept, and the rest are
		// counted as omitted. Fifteen global directives, so the three oldest go.
		for i := 1; i <= 15; i++ {
			must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: fmt.Sprintf("g%02d", i), Rule: "r"}))(t)
		}
		var want []string
		for i := 15; i >= 4; i-- {
			want = append(want, fmt.Sprintf("g%02d", i))
		}
		list := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
		if got := directiveTitles(list.Directives); !slices.Equal(got, want) {
			t.Errorf("directive_list = %v, want %v", got, want)
		}
		if list.Omitted != 3 || list.Expired != 0 {
			t.Errorf("directive_list omitted=%d expired=%d, want 3 and 0", list.Omitted, list.Expired)
		}
		cur := must(s.TaskCurrent(mainCtx()))(t)
		if len(cur.Directives) != 12 || cur.Omitted != 3 {
			t.Errorf("task_current gave %d directives, omitted=%d; want 12 and 3", len(cur.Directives), cur.Omitted)
		}
		rec := must(s.Recall(mainCtx(), core.RecallRequest{Goal: "anything"}))(t)
		if len(rec.Directives) != 12 || rec.Omitted != 3 {
			t.Errorf("recall gave %d directives, omitted=%d; want 12 and 3", len(rec.Directives), rec.Omitted)
		}
	}},
	{"directive/ranking_agent_project_global", func(t T, s core.Service) {
		// CONTRACT §5: agent-scoped first, then project, then global, and newest
		// first within each scope. Ratifications are in time order, so newest
		// first is the reverse of ratification order inside a scope.
		seedProject(t, s, projA)
		must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "g1", Rule: "r"}))(t)
		must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeProject, Project: projA, Title: "p1", Rule: "r"}))(t)
		must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeAgent, Agent: "main", Title: "a1", Rule: "r"}))(t)
		must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeProject, Project: projA, Title: "p2", Rule: "r"}))(t)
		must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "g2", Rule: "r"}))(t)
		list := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
		if got := directiveTitles(list.Directives); !slices.Equal(got, []string{"a1", "p2", "p1", "g2", "g1"}) {
			t.Errorf("directive_list order = %v, want [a1 p2 p1 g2 g1]", got)
		}
		cur := must(s.TaskCurrent(mainCtx()))(t)
		if got := directiveTitles(cur.Directives); !slices.Equal(got, []string{"a1", "p2", "p1", "g2", "g1"}) {
			t.Errorf("task_current order = %v, want [a1 p2 p1 g2 g1]", got)
		}
	}},
	{"directive/other_role_does_not_crowd_the_cap", func(t T, s core.Service) {
		// CONTRACT §2, §5, §9 item 49: task_current and recall give the agent
		// scope of the caller's own role. Twelve reviewer directives do not push out
		// the project and global ones of a main session, and a caller with no role
		// gets none of them.
		seedProject(t, s, projA)
		must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "g1", Rule: "r"}))(t)
		must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeProject, Project: projA, Title: "p1", Rule: "r"}))(t)
		for i := 1; i <= 12; i++ {
			must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeAgent, Agent: "reviewer", Title: fmt.Sprintf("r%02d", i), Rule: "r"}))(t)
		}
		want := []string{"p1", "g1"}
		cur := must(s.TaskCurrent(mainCtx()))(t)
		if got := directiveTitles(cur.Directives); !slices.Equal(got, want) || cur.Omitted != 0 {
			t.Errorf("task_current for main = %v omitted %d, want %v and omitted 0", got, cur.Omitted, want)
		}
		rec := must(s.Recall(mainCtx(), core.RecallRequest{Goal: "anything"}))(t)
		if got := directiveTitles(rec.Directives); !slices.Equal(got, want) || rec.Omitted != 0 {
			t.Errorf("recall for main = %v omitted %d, want %v and omitted 0", got, rec.Omitted, want)
		}
		none := must(s.TaskCurrent(as(projA, "")))(t)
		if got := directiveTitles(none.Directives); !slices.Equal(got, want) {
			t.Errorf("task_current with no role = %v, want %v", got, want)
		}
		list := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{Agent: "main"}))(t)
		if got := directiveTitles(list.Directives); !slices.Equal(got, want) {
			t.Errorf("directive_list agent=main = %v, want %v", got, want)
		}
	}},
	{"directive/review_by_defaults_ninety_days", func(t T, s core.Service) {
		// CONTRACT §8, decision D16: with no review_by, the directive is due 90
		// days after ratification. The window allows for the time the call took.
		before := time.Now().UTC()
		d := must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t", Rule: "r"}))(t)
		after := time.Now().UTC()
		if d.ReviewBy == nil {
			t.Fatalf("ratified directive has no review_by: %+v", d)
		}
		lo := before.AddDate(0, 0, core.DirectiveReviewDays).Add(-time.Second)
		hi := after.AddDate(0, 0, core.DirectiveReviewDays).Add(time.Second)
		if d.ReviewBy.Before(lo) || d.ReviewBy.After(hi) {
			t.Errorf("review_by = %v, want between %v and %v", d.ReviewBy, lo, hi)
		}
	}},
	{"directive/review_by_explicit_day", func(t T, s core.Service) {
		// CONTRACT §8: an explicit review_by is a UTC day, kept as its start. The
		// year is far off so the case holds whenever it runs.
		d := must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t", Rule: "r", ReviewBy: "2099-12-31"}))(t)
		want := time.Date(2099, 12, 31, 0, 0, 0, 0, time.UTC)
		if d.ReviewBy == nil || !d.ReviewBy.Equal(want) {
			t.Errorf("review_by = %v, want %v", d.ReviewBy, want)
		}
		if got := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t); got.Expired != 0 || len(got.Directives) != 1 {
			t.Errorf("a directive due in 2099 gave expired=%d and %d listed", got.Expired, len(got.Directives))
		}
	}},
	{"directive/review_by_past_or_malformed_refused", func(t T, s core.Service) {
		// CONTRACT §8: a review_by that is not a YYYY-MM-DD day, or not after
		// today, is invalid. No directive is written.
		for _, v := range []string{"2000-01-01", "2099-13-01", "31/12/2099", "2099-12-31T00:00:00Z"} {
			_, err := s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t", Rule: "r", ReviewBy: v})
			wantKind(t, err, core.KindInvalid, "review_by "+v)
		}
		if got := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t); len(got.Directives) != 0 {
			t.Errorf("refused review_by still wrote %d directives", len(got.Directives))
		}
	}},
	{"directive/reaffirm_moves_review_by_only", func(t T, s core.Service) {
		// CONTRACT §8, §9 item 52: a reaffirm sets review_by in place. The code, rule,
		// scope and ratification stay as they were, and the directive keeps its place.
		d := must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t", Rule: "the rule", ReviewBy: "2099-12-31"}))(t)
		got := must(s.DirectiveReaffirm(op(), core.DirectiveReaffirmRequest{Code: d.Code, ReviewBy: "2100-06-30"}))(t)
		want := time.Date(2100, 6, 30, 0, 0, 0, 0, time.UTC)
		if got.ReviewBy == nil || !got.ReviewBy.Equal(want) {
			t.Errorf("reaffirmed review_by = %v, want %v", got.ReviewBy, want)
		}
		if got.Code != d.Code || got.Rule != "the rule" || got.Scope != core.ScopeGlobal || got.Status != core.DirectiveActive {
			t.Errorf("reaffirm changed the directive: %+v", got)
		}
		if !got.RatifiedAt.Equal(d.RatifiedAt) || got.RatifiedBy != d.RatifiedBy {
			t.Errorf("reaffirm changed the ratification: at %v by %q, was at %v by %q",
				got.RatifiedAt, got.RatifiedBy, d.RatifiedAt, d.RatifiedBy)
		}
		list := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
		if len(list.Directives) != 1 || list.Directives[0].ReviewBy == nil || !list.Directives[0].ReviewBy.Equal(want) {
			t.Errorf("directive_list after the reaffirm = %+v, want one directive due %v", list.Directives, want)
		}
	}},
	{"directive/reaffirm_default_is_ninety_days", func(t T, s core.Service) {
		// CONTRACT §8: with no review_by, a reaffirm sets it 90 days from now, as ratify does.
		d := must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t", Rule: "r", ReviewBy: "2099-12-31"}))(t)
		before := time.Now().UTC()
		got := must(s.DirectiveReaffirm(op(), core.DirectiveReaffirmRequest{Code: d.Code}))(t)
		after := time.Now().UTC()
		if got.ReviewBy == nil {
			t.Fatalf("reaffirmed directive has no review_by")
		}
		lo := before.AddDate(0, 0, core.DirectiveReviewDays).Add(-time.Second)
		hi := after.AddDate(0, 0, core.DirectiveReviewDays).Add(time.Second)
		if got.ReviewBy.Before(lo) || got.ReviewBy.After(hi) {
			t.Errorf("reaffirmed review_by = %v, want between %v and %v", got.ReviewBy, lo, hi)
		}
	}},
	{"directive/reaffirm_refuses_retired_and_bad_input", func(t T, s core.Service) {
		// CONTRACT §8: a retired directive is refused; a bad code or review_by is invalid;
		// a code that names no row is not_found. None of them changes the directive.
		d := must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t", Rule: "r", ReviewBy: "2099-12-31"}))(t)
		_, err := s.DirectiveReaffirm(op(), core.DirectiveReaffirmRequest{Code: "nope"})
		wantKind(t, err, core.KindInvalid, "reaffirm with a bad code")
		_, err = s.DirectiveReaffirm(op(), core.DirectiveReaffirmRequest{Code: d.Code, ReviewBy: "2000-01-01"})
		wantKind(t, err, core.KindInvalid, "reaffirm to a past day")
		_, err = s.DirectiveReaffirm(op(), core.DirectiveReaffirmRequest{Code: d.Code, ReviewBy: "31/12/2099"})
		wantKind(t, err, core.KindInvalid, "reaffirm to a malformed day")
		_, err = s.DirectiveReaffirm(op(), core.DirectiveReaffirmRequest{Code: "D-9999"})
		wantKind(t, err, core.KindNotFound, "reaffirm of an unknown code")
		must(s.DirectiveRetire(op(), core.DirectiveRetireRequest{Code: d.Code}))(t)
		_, err = s.DirectiveReaffirm(op(), core.DirectiveReaffirmRequest{Code: d.Code, ReviewBy: "2100-01-01"})
		wantKind(t, err, core.KindRefused, "reaffirm of a retired directive")
	}},
	{"directive/retire_checks_the_code_form", func(t T, s core.Service) {
		// CONTRACT §8 (issue #163): retire checks a code's form as reaffirm does, so a
		// malformed code is invalid. A well-formed code with no row is not_found.
		for _, code := range []string{"nope", "D-1"} {
			_, err := s.DirectiveRetire(op(), core.DirectiveRetireRequest{Code: code})
			wantKind(t, err, core.KindInvalid, "retire of malformed code "+code)
		}
		_, err := s.DirectiveRetire(op(), core.DirectiveRetireRequest{Code: "D-9999"})
		wantKind(t, err, core.KindNotFound, "retire of an unknown code")
	}},
	{"directive/operator_calls_need_a_project_row", func(t T, s core.Service) {
		// CONTRACT §8 (issue #164): a project-scope ratify and a settings save that name a
		// project with no row are not_found, and create no row. An agent's first call
		// creates the row, and the same calls then succeed.
		_, err := s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeProject, Project: projA, Title: "p", Rule: "r"})
		wantKind(t, err, core.KindNotFound, "project directive ratified for a project with no row")
		_, err = s.ProjectSettingsSet(op(), core.ProjectSettingsRequest{Project: projA, Settings: core.DefaultSettings().AsInput()})
		wantKind(t, err, core.KindNotFound, "settings saved for a project with no row")
		_, err = s.ProjectList(op(), core.ProjectListRequest{Project: projA})
		wantKind(t, err, core.KindNotFound, "the refused calls left no project row")

		seedProject(t, s, projA)
		must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeProject, Project: projA, Title: "p", Rule: "r"}))(t)
		must(s.ProjectSettingsSet(op(), core.ProjectSettingsRequest{Project: projA, Settings: core.DefaultSettings().AsInput()}))(t)
	}},
}
