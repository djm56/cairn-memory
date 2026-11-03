package core_test

import (
	"context"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// reaffirmHistory returns the number of directive_reaffirmations rows of the directive with code,
// and the previous and new review_by of its newest row, as stored.
func reaffirmHistory(t *testing.T, st *store.Store, code string) (n int, prev, now, by string) {
	t.Helper()
	ctx := context.Background()
	if err := st.QueryRowContext(ctx, `SELECT COUNT(*) FROM directive_reaffirmations r
		JOIN directives d ON d.id = r.directive_id WHERE d.code = ?`, code).Scan(&n); err != nil {
		t.Fatalf("count reaffirmations: %v", err)
	}
	if n == 0 {
		return 0, "", "", ""
	}
	if err := st.QueryRowContext(ctx, `SELECT r.previous_review_by, r.review_by, r.reaffirmed_by
		FROM directive_reaffirmations r JOIN directives d ON d.id = r.directive_id
		WHERE d.code = ? ORDER BY r.id DESC LIMIT 1`, code).Scan(&prev, &now, &by); err != nil {
		t.Fatalf("read reaffirmation: %v", err)
	}
	return n, prev, now, by
}

// storedReviewBy returns the review_by of the directive with code, as the store holds the text.
func storedReviewBy(t *testing.T, st *store.Store, code string) string {
	t.Helper()
	var s string
	if err := st.QueryRowContext(context.Background(),
		`SELECT review_by FROM directives WHERE code = ?`, code).Scan(&s); err != nil {
		t.Fatalf("read review_by of %s: %v", code, err)
	}
	return s
}

// TestDirectiveReaffirmWritesOneHistoryRow checks that a reaffirm moves review_by in place and
// writes exactly one history row in the same transaction: the day it had, the day it has now, and
// the operator's name. The code and the ratification stay as they were.
func TestDirectiveReaffirmWritesOneHistoryRow(t *testing.T) {
	s, st := openLocal(t)
	clockAt(s, t0)
	d := must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{
		Scope: core.ScopeGlobal, Title: "t", Rule: "r", ReviewBy: "2099-12-31"}))(t)
	before := storedReviewBy(t, st, d.Code)

	clockAt(s, t0.Add(time.Hour))
	got := must(s.DirectiveReaffirm(opCtx(), core.DirectiveReaffirmRequest{Code: d.Code, ReviewBy: "2100-06-30"}))(t)

	if got.Code != d.Code {
		t.Errorf("code changed: %s, was %s", got.Code, d.Code)
	}
	if got.Rule != "r" || !got.RatifiedAt.Equal(d.RatifiedAt) || got.RatifiedBy != d.RatifiedBy {
		t.Errorf("the ratification or the rule changed: %+v", got)
	}
	n, prev, now, by := reaffirmHistory(t, st, d.Code)
	if n != 1 {
		t.Errorf("history row count = %d, want 1", n)
	}
	if prev != before {
		t.Errorf("previous_review_by = %q, want the review_by it had, %q", prev, before)
	}
	if now != storedReviewBy(t, st, d.Code) {
		t.Errorf("review_by of the history row = %q, want the directive's review_by %q", now, storedReviewBy(t, st, d.Code))
	}
	if by != "operator" {
		t.Errorf("reaffirmed_by = %q, want the operator's name", by)
	}
}

// TestDirectiveReaffirmRefusesRetiredAndWritesNothing checks that a retired directive is refused
// and that the refusal writes no history row and moves no review_by.
func TestDirectiveReaffirmRefusesRetiredAndWritesNothing(t *testing.T) {
	s, st := openLocal(t)
	clockAt(s, t0)
	d := ratifyGlobal(t, s, "t")
	must(s.DirectiveRetire(opCtx(), core.DirectiveRetireRequest{Code: d.Code}))(t)
	before := storedReviewBy(t, st, d.Code)

	_, err := s.DirectiveReaffirm(opCtx(), core.DirectiveReaffirmRequest{Code: d.Code, ReviewBy: "2100-01-01"})
	wantKindLocal(t, err, core.KindRefused, "reaffirm of a retired directive")
	if n, _, _, _ := reaffirmHistory(t, st, d.Code); n != 0 {
		t.Errorf("a refused reaffirm wrote %d history rows", n)
	}
	if storedReviewBy(t, st, d.Code) != before {
		t.Errorf("a refused reaffirm moved review_by")
	}
}

// TestDirectiveReaffirmAllowsAnExpiredDirectiveAndDefaultsNinetyDays checks that an expired directive
// is reaffirmed like any other, and that with no review_by the new one is 90 days from now. An
// explicit day that is not after today is invalid, and it writes nothing.
func TestDirectiveReaffirmAllowsAnExpiredDirectiveAndDefaultsNinetyDays(t *testing.T) {
	s, st := openLocal(t)
	clockAt(s, t0)
	d := must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{
		Scope: core.ScopeGlobal, Title: "t", Rule: "r", ReviewBy: "2026-11-05"}))(t)

	// Ten days on, the directive is past its review_by.
	later := t0.AddDate(0, 0, 10)
	clockAt(s, later)
	listed := must(s.DirectiveBrowse(opCtx(), core.DirectiveBrowseRequest{State: core.DirectiveActive}))(t)
	if len(listed.Directives) != 1 || !listed.Directives[0].Expired {
		t.Fatalf("the directive is not expired before the reaffirm: %+v", listed.Directives)
	}

	_, err := s.DirectiveReaffirm(opCtx(), core.DirectiveReaffirmRequest{Code: d.Code, ReviewBy: "2026-10-31"})
	wantKindLocal(t, err, core.KindInvalid, "reaffirm to a day before now")
	if n, _, _, _ := reaffirmHistory(t, st, d.Code); n != 0 {
		t.Errorf("a refused day wrote %d history rows", n)
	}

	got := must(s.DirectiveReaffirm(opCtx(), core.DirectiveReaffirmRequest{Code: d.Code}))(t)
	want := later.AddDate(0, 0, core.DirectiveReviewDays)
	if got.ReviewBy == nil || !got.ReviewBy.Equal(want) {
		t.Errorf("default review_by = %v, want %v", got.ReviewBy, want)
	}
	listed = must(s.DirectiveBrowse(opCtx(), core.DirectiveBrowseRequest{State: core.DirectiveActive}))(t)
	if len(listed.Directives) != 1 || listed.Directives[0].Expired {
		t.Errorf("the reaffirmed directive is still expired: %+v", listed.Directives)
	}
}
