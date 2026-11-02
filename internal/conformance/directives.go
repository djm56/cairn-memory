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
}
