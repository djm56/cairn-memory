package core_test

import (
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestRecallRanksBestMatchFirst checks that recall puts the lesson with more of
// the goal's words first, and that limit cuts the list after the best match.
// The goal has two usable words, JWT and expiry. The first lesson holds only
// JWT; the second holds both, so it is the better match.
func TestRecallRanksBestMatchFirst(t *testing.T) {
	s, st := openLocal(t)
	// One task per lesson: seedLesson replaces a task's lessons, so a second
	// call on the same task would drop the first.
	first := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Key rotation", Goal: "Rotate keys.", Queue: true}))(t)
	seedLesson(t, st, first.ID, "Rotate the JWT signing key before release.")
	second := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Expiry checks", Goal: "Check expiry.", Queue: true}))(t)
	seedLesson(t, st, second.ID, "A JWT whose expiry has passed must be refused.")

	got := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "JWT expiry checks"})
	if len(got.Lessons) != 2 {
		t.Fatalf("recall lessons = %+v, want both", got.Lessons)
	}
	if got.Lessons[0].Text != "A JWT whose expiry has passed must be refused." {
		t.Errorf("recall lessons[0] = %q, want the lesson that holds both words", got.Lessons[0].Text)
	}

	one := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "JWT expiry checks", Limit: 1})
	if len(one.Lessons) != 1 || one.Lessons[0].Text != "A JWT whose expiry has passed must be refused." {
		t.Errorf("recall with limit 1 = %+v, want only the best match", one.Lessons)
	}
}

// TestRecallKeepsShortNameWords checks the short words that name a subject:
// upper-case or digit-bearing words under four letters. "Fix CSS bug" must
// keep CSS, so a lesson about CSS is found; "Fix" is a common word and is not.
func TestRecallKeepsShortNameWords(t *testing.T) {
	s, st := openLocal(t)
	res := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Style work", Goal: "Tidy the stylesheet."}))(t)
	seedLesson(t, st, res.ID, "Scope every CSS rule to its component.")

	got := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "Fix CSS bug"})
	if len(got.Lessons) != 1 {
		t.Errorf("recall of \"Fix CSS bug\" lessons = %+v, want the CSS lesson", got.Lessons)
	}
	// A lower-case short word is not a name, so a goal of such words matches nothing.
	none := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "fix bug"})
	if len(none.Lessons) != 0 {
		t.Errorf("recall of \"fix bug\" lessons = %+v, want none", none.Lessons)
	}
}
