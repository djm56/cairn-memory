package core_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// t0 is the server's time at the start of each directive test. The clock seam
// holds it still, so two directives ratified in one test share a ratified_at
// and the id tiebreak is what orders them.
var t0 = time.Date(2026, 10, 31, 10, 0, 0, 0, time.UTC)

// clockAt sets the service's clock to at, and returns the same service.
func clockAt(s *core.Local, at time.Time) *core.Local {
	s.SetClock(func() time.Time { return at })
	return s
}

// ratifyGlobal ratifies a global directive titled title, with no review_by.
func ratifyGlobal(t *testing.T, s core.Service, title string) *core.Directive {
	t.Helper()
	return must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: title, Rule: "r"}))(t)
}

// TestDirectiveRankingAcrossScopes checks the order the three reads give:
// agent-scoped first, then project, then global, and newest first within each.
// Directives ratified at the same instant tie on ratified_at, and the higher id
// comes first (decision D16).
func TestDirectiveRankingAcrossScopes(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	ratifyGlobal(t, s, "g1")
	must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeProject, Project: projA, Title: "p1", Rule: "r"}))(t)
	must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeAgent, Agent: "main", Title: "a1", Rule: "r"}))(t)
	must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeProject, Project: projA, Title: "p2", Rule: "r"}))(t)
	ratifyGlobal(t, s, "g2")

	// All four were ratified at t0. Ties resolve by id, newest first.
	got := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
	if titles := directiveTitles(got.Directives); !slices.Equal(titles, []string{"a1", "p2", "p1", "g2", "g1"}) {
		t.Fatalf("tied order = %v, want [a1 p2 p1 g2 g1]", titles)
	}

	// A later ratification is newer than the ties in its own scope, and still
	// ranks below every agent and project directive.
	clockAt(s, t0.Add(time.Hour))
	ratifyGlobal(t, s, "g3")
	got = must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
	if titles := directiveTitles(got.Directives); !slices.Equal(titles, []string{"a1", "p2", "p1", "g3", "g2", "g1"}) {
		t.Errorf("newer order = %v, want [a1 p2 p1 g3 g2 g1]", titles)
	}
}

// TestDirectiveCapAtTwelve checks that at most DirectiveCap directives come back
// from each of the three reads, that the newest ones are the ones kept, and that
// the rest are counted as omitted.
func TestDirectiveCapAtTwelve(t *testing.T) {
	if core.DirectiveCap != 12 {
		t.Fatalf("DirectiveCap = %d, want 12 (decision D16)", core.DirectiveCap)
	}
	s, _ := openLocal(t)
	clockAt(s, t0)
	for i := 1; i <= 15; i++ {
		ratifyGlobal(t, s, fmt.Sprintf("g%02d", i))
	}

	// Ties on ratified_at give id order, newest first: g15 down to g04 are kept,
	// and g01 to g03 are the three omitted.
	var want []string
	for i := 15; i >= 4; i-- {
		want = append(want, fmt.Sprintf("g%02d", i))
	}

	list := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
	if got := directiveTitles(list.Directives); !slices.Equal(got, want) {
		t.Errorf("directive_list = %v, want %v", got, want)
	}
	if list.Omitted != 3 || list.Expired != 0 {
		t.Errorf("directive_list omitted=%d expired=%d, want omitted=3 expired=0", list.Omitted, list.Expired)
	}

	cur := must(s.TaskCurrent(mainCtx()))(t)
	if len(cur.Directives) != 12 || cur.Omitted != 3 {
		t.Errorf("task_current gave %d directives, omitted=%d; want 12 and 3", len(cur.Directives), cur.Omitted)
	}

	rec := must(s.Recall(mainCtx(), core.RecallRequest{Goal: "anything"}))(t)
	if len(rec.Directives) != 12 || rec.Omitted != 3 {
		t.Errorf("recall gave %d directives, omitted=%d; want 12 and 3", len(rec.Directives), rec.Omitted)
	}
}

// TestExpiredDirectivesAreLeftOutAndCounted checks that a directive past its
// review_by is left out of all three reads and counted as expired, that a
// directive not yet due stays, and that a retired one is neither listed nor
// counted.
func TestExpiredDirectivesAreLeftOutAndCounted(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{
		Scope: core.ScopeGlobal, Title: "due-soon", Rule: "r", ReviewBy: "2026-11-05"}))(t)
	ratifyGlobal(t, s, "due-later")
	retired := must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{
		Scope: core.ScopeGlobal, Title: "retired", Rule: "r", ReviewBy: "2026-11-05"}))(t)
	must(s.DirectiveRetire(opCtx(), core.DirectiveRetireRequest{Code: retired.Code}))(t)

	// Before the day: nothing has expired.
	before := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
	if !slices.Equal(directiveTitles(before.Directives), []string{"due-later", "due-soon"}) || before.Expired != 0 {
		t.Fatalf("before expiry: %v expired=%d", directiveTitles(before.Directives), before.Expired)
	}

	// Past due-soon's review_by, before due-later's.
	clockAt(s, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC))
	list := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
	if got := directiveTitles(list.Directives); !slices.Equal(got, []string{"due-later"}) {
		t.Errorf("directive_list after expiry = %v, want [due-later]", got)
	}
	if list.Expired != 1 || list.Omitted != 0 {
		t.Errorf("directive_list expired=%d omitted=%d, want expired=1 omitted=0", list.Expired, list.Omitted)
	}
	cur := must(s.TaskCurrent(mainCtx()))(t)
	if len(cur.Directives) != 1 || cur.Expired != 1 {
		t.Errorf("task_current gave %d directives, expired=%d; want 1 and 1", len(cur.Directives), cur.Expired)
	}
	rec := must(s.Recall(mainCtx(), core.RecallRequest{Goal: "anything"}))(t)
	if len(rec.Directives) != 1 || rec.Expired != 1 {
		t.Errorf("recall gave %d directives, expired=%d; want 1 and 1", len(rec.Directives), rec.Expired)
	}
}

// TestRatifyDefaultsReviewByTo90Days checks that a ratify call with no review_by
// gives 90 days after the ratification time, and that the value reads back.
func TestRatifyDefaultsReviewByTo90Days(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	d := ratifyGlobal(t, s, "default")
	want := t0.AddDate(0, 0, 90)
	if d.ReviewBy == nil || !d.ReviewBy.Equal(want) {
		t.Fatalf("ratified review_by = %v, want %v", d.ReviewBy, want)
	}
	list := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
	if len(list.Directives) != 1 || list.Directives[0].ReviewBy == nil || !list.Directives[0].ReviewBy.Equal(want) {
		t.Errorf("read back review_by = %+v, want %v", list.Directives, want)
	}
}

// TestRatifyAcceptsExplicitReviewBy checks that a future calendar day is kept as
// the review date, as the start of that day in UTC.
func TestRatifyAcceptsExplicitReviewBy(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	d := must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{
		Scope: core.ScopeGlobal, Title: "explicit", Rule: "r", ReviewBy: "2026-12-31"}))(t)
	want := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	if d.ReviewBy == nil || !d.ReviewBy.Equal(want) {
		t.Errorf("review_by = %v, want %v", d.ReviewBy, want)
	}
}

// TestRatifyRefusesBadReviewBy checks that a review_by that is malformed, or not
// after today, is refused as invalid and writes no directive. Today itself is
// refused: its start is already past.
func TestRatifyRefusesBadReviewBy(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	cases := []struct {
		name, value string
	}{
		{"today", "2026-10-31"},
		{"yesterday", "2026-10-30"},
		{"day first", "31/12/2026"},
		{"timestamp", "2026-12-31T00:00:00Z"},
		{"nonsense", "soon"},
	}
	for _, c := range cases {
		_, err := s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{
			Scope: core.ScopeGlobal, Title: "t", Rule: "r", ReviewBy: c.value})
		wantKindLocal(t, err, core.KindInvalid, "review_by "+c.name)
	}
	list := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
	if len(list.Directives) != 0 {
		t.Errorf("refused review_by still wrote %d directives", len(list.Directives))
	}
}

// ratifyAgent ratifies a directive scoped to role, with no review_by.
func ratifyAgent(t *testing.T, s core.Service, role, title string) {
	t.Helper()
	must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeAgent, Agent: role, Title: title, Rule: "r"}))(t)
}

// TestOtherRoleDirectivesDoNotCrowdTheCap is 12-W1. Twelve reviewer directives must not
// push out the project and global directives of a main-session caller, from task_current
// or recall. A caller with no role gets none of the reviewer's. The reviewer, asking for
// its own, gets its twelve first. directive_list with no agent is the explicit listing of
// every role, so there the reviewer's directives do fill the cap: that is pinned too.
func TestOtherRoleDirectivesDoNotCrowdTheCap(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	ratifyGlobal(t, s, "g1")
	ratifyGlobal(t, s, "g2")
	must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeProject, Project: projA, Title: "p1", Rule: "r"}))(t)
	must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeProject, Project: projA, Title: "p2", Rule: "r"}))(t)
	for i := 1; i <= 12; i++ {
		ratifyAgent(t, s, "reviewer", fmt.Sprintf("r%02d", i))
	}
	mine := []string{"p2", "p1", "g2", "g1"}

	cur := must(s.TaskCurrent(mainCtx()))(t)
	if got := directiveTitles(cur.Directives); !slices.Equal(got, mine) || cur.Omitted != 0 {
		t.Errorf("task_current for main = %v omitted %d, want %v and omitted 0", got, cur.Omitted, mine)
	}
	rec := must(s.Recall(mainCtx(), core.RecallRequest{Goal: "anything"}))(t)
	if got := directiveTitles(rec.Directives); !slices.Equal(got, mine) || rec.Omitted != 0 {
		t.Errorf("recall for main = %v omitted %d, want %v and omitted 0", got, rec.Omitted, mine)
	}
	noRole := must(s.TaskCurrent(agent(projA, "")))(t)
	if got := directiveTitles(noRole.Directives); !slices.Equal(got, mine) || noRole.Omitted != 0 {
		t.Errorf("task_current with no role = %v omitted %d, want %v and omitted 0", got, noRole.Omitted, mine)
	}

	own := must(s.TaskCurrent(agent(projA, "reviewer")))(t)
	if len(own.Directives) != 12 || own.Directives[0].Title != "r12" || own.Omitted != 4 {
		t.Errorf("task_current for reviewer gave %d directives, first %q, omitted %d; want 12, r12 first, omitted 4",
			len(own.Directives), own.Directives[0].Title, own.Omitted)
	}

	mineList := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{Agent: "main"}))(t)
	if got := directiveTitles(mineList.Directives); !slices.Equal(got, mine) || mineList.Omitted != 0 {
		t.Errorf("directive_list agent=main = %v omitted %d, want %v and omitted 0", got, mineList.Omitted, mine)
	}
	every := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
	if len(every.Directives) != 12 || every.Directives[0].Title != "r12" || every.Omitted != 4 {
		t.Errorf("directive_list with no agent gave %d, first %q, omitted %d; want the explicit every-role listing: 12, r12 first, omitted 4",
			len(every.Directives), every.Directives[0].Title, every.Omitted)
	}
}

// TestDirectiveWithNoReviewByIsNeverExpired is 12-S1: an active directive with a NULL
// review_by is live, however late the clock runs. No call writes one, so the row is
// written through the store.
func TestDirectiveWithNoReviewByIsNeverExpired(t *testing.T) {
	s, st := openLocal(t)
	ctx := context.Background()
	at := t0
	if err := st.WithTx(ctx, func(q store.Querier) error {
		code, err := store.NextDirectiveCode(ctx, q)
		if err != nil {
			return err
		}
		_, err = store.InsertDirective(ctx, q, store.DirectiveRow{Code: code, Scope: "global", Title: "no-review",
			Rule: "r", Status: "active", RatifiedBy: "operator", RatifiedAt: &at})
		return err
	}); err != nil {
		t.Fatalf("insert a directive with no review_by: %v", err)
	}
	clockAt(s, time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC))
	list := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
	if got := directiveTitles(list.Directives); !slices.Equal(got, []string{"no-review"}) || list.Expired != 0 {
		t.Errorf("directive with no review_by: listed %v, expired %d; want it listed and nothing expired", got, list.Expired)
	}
}

// TestReviewByBoundary is 12-S1 at the edge. A directive whose review_by is 2026-11-05 is
// live at 00:00:00 UTC that day, which is review_by == now, and expired one nanosecond
// later (CONTRACT §8: it expires at 00:00 UTC of the named day).
func TestReviewByBoundary(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{
		Scope: core.ScopeGlobal, Title: "due", Rule: "r", ReviewBy: "2026-11-05"}))(t)
	day := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)

	clockAt(s, day)
	live := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
	if got := directiveTitles(live.Directives); !slices.Equal(got, []string{"due"}) || live.Expired != 0 {
		t.Errorf("at review_by == now: listed %v, expired %d; want it listed, expired 0", got, live.Expired)
	}

	clockAt(s, day.Add(time.Nanosecond))
	gone := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
	if len(gone.Directives) != 0 || gone.Expired != 1 {
		t.Errorf("one nanosecond past review_by: listed %d, expired %d; want 0 listed, 1 expired", len(gone.Directives), gone.Expired)
	}
}

// TestExpiredDirectivesDoNotTakeCapSlots is 12-S1. Fourteen live and two expired global
// directives: the expired two are left out before the cap, so twelve are listed, two are
// omitted, and two are counted as expired, in every read.
func TestExpiredDirectivesDoNotTakeCapSlots(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	for _, title := range []string{"old1", "old2"} {
		must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{
			Scope: core.ScopeGlobal, Title: title, Rule: "r", ReviewBy: "2026-11-05"}))(t)
	}
	for i := 1; i <= 14; i++ {
		ratifyGlobal(t, s, fmt.Sprintf("g%02d", i))
	}
	clockAt(s, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC))

	// Ties on ratified_at give id order, newest first: g14 down to g03 are kept.
	var want []string
	for i := 14; i >= 3; i-- {
		want = append(want, fmt.Sprintf("g%02d", i))
	}
	list := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
	if got := directiveTitles(list.Directives); !slices.Equal(got, want) || list.Omitted != 2 || list.Expired != 2 {
		t.Errorf("directive_list = %v omitted %d expired %d; want the twelve newest, omitted 2, expired 2", got, list.Omitted, list.Expired)
	}
	cur := must(s.TaskCurrent(mainCtx()))(t)
	if got := directiveTitles(cur.Directives); !slices.Equal(got, want) || cur.Omitted != 2 || cur.Expired != 2 {
		t.Errorf("task_current = %v omitted %d expired %d; want the twelve newest, omitted 2, expired 2", got, cur.Omitted, cur.Expired)
	}
}

// TestSearchStillFindsAnExpiredDirective is 12-S2. search does not use the capped set, and
// it still returns an expired directive, so a lapsed rule can be found and reaffirmed. The
// directive is left out of the three reads, and search is the one read that finds it.
func TestSearchStillFindsAnExpiredDirective(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{
		Scope: core.ScopeGlobal, Title: "lapsed rule", Rule: "r", ReviewBy: "2026-11-05"}))(t)
	clockAt(s, time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC))

	list := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
	if len(list.Directives) != 0 || list.Expired != 1 {
		t.Fatalf("directive_list: listed %d, expired %d; want 0 and 1", len(list.Directives), list.Expired)
	}
	res := must(s.Search(mainCtx(), core.SearchRequest{Query: "lapsed", Kinds: []core.HitKind{core.HitDirective}}))(t)
	if len(res.Hits) != 1 || res.Hits[0].Kind != core.HitDirective {
		t.Errorf("search for an expired directive = %+v, want one directive hit", res.Hits)
	}
}
