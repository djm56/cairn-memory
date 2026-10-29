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

// TestRecallIgnoresFunctionWordsInCapitals checks that a goal written in capitals
// does not pull in rows by its function words. "THE" passes the upper-case test
// for short words, so recall must drop it by its stop-list entry. The lesson
// that holds only "the" must not be found; the lesson with "login" must.
func TestRecallIgnoresFunctionWordsInCapitals(t *testing.T) {
	s, st := openLocal(t)
	first := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Cache", Goal: "Cache work.", Queue: true}))(t)
	seedLesson(t, st, first.ID, "Keep the cache warm.")
	second := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Auth", Goal: "Auth work.", Queue: true}))(t)
	seedLesson(t, st, second.ID, "The login form is fine.")

	got := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "FIX THE LOGIN BUG"})
	if len(got.Lessons) != 1 || got.Lessons[0].Text != "The login form is fine." {
		t.Errorf("recall of \"FIX THE LOGIN BUG\" lessons = %+v, want only the login lesson", got.Lessons)
	}
}

// TestRecallKeepsShortNameWords checks the short words that name a subject:
// upper-case or digit-bearing words under four letters. "Fix CSS bug" must
// keep CSS, so a lesson about CSS is found; "Fix" is a common word and is not.
func TestRecallKeepsShortNameWords(t *testing.T) {
	s, st := openLocal(t)
	// One task per lesson: seedLesson replaces a task's lessons.
	res := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Style work", Goal: "Tidy the stylesheet."}))(t)
	seedLesson(t, st, res.ID, "Scope every CSS rule to its component.")
	// This lesson holds "Fix" and no CSS or bug. If "fix" were kept in a goal, it
	// would match, so the two halves of the test below can fail.
	second := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Build work", Goal: "Repair the build.", Queue: true}))(t)
	seedLesson(t, st, second.ID, "Fix the build before merge.")

	got := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "Fix CSS bug"})
	if len(got.Lessons) != 1 || got.Lessons[0].Text != "Scope every CSS rule to its component." {
		t.Errorf("recall of \"Fix CSS bug\" lessons = %+v, want only the CSS lesson", got.Lessons)
	}
	// A lower-case short word is not a name, so a goal of such words matches nothing.
	none := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "fix bug"})
	if len(none.Lessons) != 0 {
		t.Errorf("recall of \"fix bug\" lessons = %+v, want none", none.Lessons)
	}
}

// TestRecallCapitalGoalDropsShortWords checks a goal written all in capitals.
// Its case says nothing about a name, so TO and API are dropped with ADD, and
// only RATE and LIMIT match. The lesson that holds only "to" and "API" must not
// be found, and the rate-limit lesson must. Under the case rule for mixed goals
// both short words would match, so the test fails if that rule is kept.
func TestRecallCapitalGoalDropsShortWords(t *testing.T) {
	s, st := openLocal(t)
	first := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Limits", Goal: "Limit work.", Queue: true}))(t)
	seedLesson(t, st, first.ID, "Rate limit every endpoint.")
	second := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Gateway", Goal: "Gateway work.", Queue: true}))(t)
	seedLesson(t, st, second.ID, "Move the header to the API gateway.")

	got := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "ADD RATE LIMIT TO API"})
	if len(got.Lessons) != 1 || got.Lessons[0].Text != "Rate limit every endpoint." {
		t.Errorf("recall of \"ADD RATE LIMIT TO API\" lessons = %+v, want only the rate-limit lesson", got.Lessons)
	}
}

// TestRecallCJKGoalKeepsShortWords checks a goal in an uncased script. It has
// no upper-case letter, so it is not a goal in capitals, and its two-character
// words are kept. The lesson holding "限流" and "配置" must be found; the
// unrelated lesson must not. Read as capitals, the goal would keep neither word.
func TestRecallCJKGoalKeepsShortWords(t *testing.T) {
	s, st := openLocal(t)
	first := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Rate", Goal: "Rate work.", Queue: true}))(t)
	seedLesson(t, st, first.ID, "设置 限流 配置 每分钟上限")
	second := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Logs", Goal: "Log work.", Queue: true}))(t)
	seedLesson(t, st, second.ID, "Unrelated lesson about logging.")

	got := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "限流 配置"})
	if len(got.Lessons) != 1 || got.Lessons[0].Text != "设置 限流 配置 每分钟上限" {
		t.Errorf("recall of \"限流 配置\" lessons = %+v, want only the rate lesson", got.Lessons)
	}
}

// TestRecallUnspacedCJKGoalKeepsWord checks an unspaced two-character goal in an
// uncased script. Without spaces the goal is one token of two characters. It is
// not in capitals, so the token is kept and finds the rate lesson. Read as
// capitals, a two-character token would be dropped and find nothing.
func TestRecallUnspacedCJKGoalKeepsWord(t *testing.T) {
	s, st := openLocal(t)
	first := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Rate", Goal: "Rate work.", Queue: true}))(t)
	seedLesson(t, st, first.ID, "设置 限流 配置 每分钟上限")
	second := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Logs", Goal: "Log work.", Queue: true}))(t)
	seedLesson(t, st, second.ID, "Unrelated lesson about logging.")

	got := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "限流"})
	if len(got.Lessons) != 1 || got.Lessons[0].Text != "设置 限流 配置 每分钟上限" {
		t.Errorf("recall of \"限流\" lessons = %+v, want only the rate lesson", got.Lessons)
	}
}

// TestRecallKeepsDigitFirstShortWord checks a two-character word that starts
// with a digit, in a goal that is not in capitals. "3d" holds a digit, so it is
// kept and finds the lesson. Mutation testing showed that the digit test was
// not pinned at the start of a word.
func TestRecallKeepsDigitFirstShortWord(t *testing.T) {
	s, st := openLocal(t)
	first := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Maps", Goal: "Map work.", Queue: true}))(t)
	seedLesson(t, st, first.ID, "Index the 3d map.")
	second := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Logs", Goal: "Log work.", Queue: true}))(t)
	seedLesson(t, st, second.ID, "Unrelated lesson about logging.")

	got := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "Build 3d maps"})
	if len(got.Lessons) != 1 || got.Lessons[0].Text != "Index the 3d map." {
		t.Errorf("recall of \"Build 3d maps\" lessons = %+v, want only the 3d lesson", got.Lessons)
	}
}

// TestRecallMixedCaseKeepsNames checks that a goal with lower-case words keeps
// its upper-case short names and its digit words: JWT, SQL, TLS, CLI and v2 each
// find their lesson. The lower-case "use" is dropped, and no lesson holds it.
func TestRecallMixedCaseKeepsNames(t *testing.T) {
	s, st := openLocal(t)
	seeds := []string{"Rotate the JWT key.", "Parameterise every SQL query.", "Pin TLS 1.3.", "Flags on the CLI.", "Keep the v2 endpoints."}
	for _, text := range seeds {
		// One task per lesson: seedLesson replaces a task's lessons.
		task := must(s.TaskStart(as(projB, "main"), core.TaskStartRequest{Title: "Lesson work", Goal: "Work.", Queue: true}))(t)
		seedLesson(t, st, task.ID, text)
	}

	got := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "Check JWT, SQL, TLS, CLI and v2 use", Limit: 10})
	found := map[string]bool{}
	for _, l := range got.Lessons {
		found[l.Text] = true
	}
	for _, text := range seeds {
		if !found[text] {
			t.Errorf("recall of a mixed-case goal did not find %q; lessons = %+v", text, got.Lessons)
		}
	}
}
