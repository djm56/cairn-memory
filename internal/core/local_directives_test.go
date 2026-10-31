package core_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
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
