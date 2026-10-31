package core_test

import (
	"context"
	"slices"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// The projects the tests in this package use. The conformance package has its
// own copies, since it is a separate package.
const (
	projA = "acme-api"
	projB = "acme-web"
)

// mainCtx is the usual caller: agent "main" in projA.
func mainCtx() context.Context { return agent(projA, "main") }

// as returns a context for agent in project.
func as(project, name string) context.Context { return agent(project, name) }

// must takes a call's results and returns a check: must(s.TaskStart(...))(t)
// fails the test when the call returned an error, and returns its value
// otherwise. Go cannot pass a multi-value call next to another argument, so the
// results come in first.
func must[V any](v V, err error) func(t *testing.T) V {
	return func(t *testing.T) V {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return v
	}
}

// mustSearch runs Search for ctx and fails the test on error.
func mustSearch(ctx context.Context, t *testing.T, s *core.Local, req core.SearchRequest) *core.SearchResult {
	t.Helper()
	return must(s.Search(ctx, req))(t)
}

// mustRecall runs Recall for ctx and fails the test on error.
func mustRecall(ctx context.Context, t *testing.T, s *core.Local, req core.RecallRequest) *core.RecallResult {
	t.Helper()
	return must(s.Recall(ctx, req))(t)
}

// seedLesson writes a lesson of a task through the store. The rows are made
// here, so the search and recall tests do not depend on task_close.
func seedLesson(t *testing.T, st *store.Store, taskID, text string) {
	t.Helper()
	ctx := context.Background()
	err := st.WithTx(ctx, func(q store.Querier) error {
		return store.ReplaceLessons(ctx, q, taskID, []store.LessonRow{
			{TaskID: taskID, Ord: 1, Kind: string(core.LessonReuse), Text: text, Why: "seeded"},
		})
	})
	if err != nil {
		t.Fatalf("seed lesson on %s: %v", taskID, err)
	}
}

// seedMistake writes a mistake of a task through the store, as seedLesson does
// for a lesson.
func seedMistake(t *testing.T, st *store.Store, taskID, text string) {
	t.Helper()
	ctx := context.Background()
	err := st.WithTx(ctx, func(q store.Querier) error {
		return store.ReplaceMistakes(ctx, q, taskID, []store.MistakeRow{
			{TaskID: taskID, Ord: 1, Agent: "main", Mistake: text, Impact: "high", Prevention: "seeded"},
		})
	})
	if err != nil {
		t.Fatalf("seed mistake on %s: %v", taskID, err)
	}
}

// TestSearchFindsEachKind checks search over every kind. Each row holds one
// word that no other row holds, so the first hit for that word is a row of its
// kind. The directive, lesson and mistake are written through the store, so the
// test does not depend on the operator and close calls.
func TestSearchFindsEachKind(t *testing.T) {
	s, st := openLocal(t)
	ctx := mainCtx()
	res := must(s.TaskStart(ctx, core.TaskStartRequest{Title: "Tune the zeppelin cache", Goal: "Cache hit rate over 90%."}))(t)
	must(s.EventLog(ctx, core.EventLogRequest{TaskID: res.ID, Kind: core.EventNote, Text: "The quokka endpoint is slow."}))(t)
	must(s.LearningAdd(ctx, core.LearningAddRequest{Kind: core.LearningPattern, Title: "Narwhal batching", Body: "Batch writes."}))(t)
	insertDirective(t, st, "global", "", "", "Pangolin rule", "active")
	seedLesson(t, st, res.ID, "Reuse the otter parser.")
	seedMistake(t, st, res.ID, "Ignored the wombat timeout.")

	want := map[string]struct {
		kind core.HitKind
		id   string // the id to check, or "" to skip the check
	}{
		"zeppelin": {core.HitTask, res.ID},
		"quokka":   {core.HitEvent, ""},
		"narwhal":  {core.HitLearning, ""},
		"pangolin": {core.HitDirective, "D-0001"},
		"otter":    {core.HitLesson, res.ID + "/1"},
		"wombat":   {core.HitMistake, res.ID + "/1"},
	}
	for word, w := range want {
		got := mustSearch(ctx, t, s, core.SearchRequest{Query: word})
		if len(got.Hits) == 0 || got.Hits[0].Kind != w.kind {
			t.Errorf("search %q = %+v, want a %s first", word, got.Hits, w.kind)
			continue
		}
		if got.Hits[0].Snippet == "" {
			t.Errorf("search %q: the hit has no snippet", word)
		}
		if w.id != "" && got.Hits[0].ID != w.id {
			t.Errorf("search %q: id %q, want %q", word, got.Hits[0].ID, w.id)
		}
	}

	// A kinds filter keeps only its kind, and an unknown kind is invalid.
	only := mustSearch(ctx, t, s, core.SearchRequest{Query: "zeppelin", Kinds: []core.HitKind{core.HitEvent}})
	if len(only.Hits) != 0 {
		t.Errorf("search limited to events found %+v", only.Hits)
	}
	_, err := s.Search(ctx, core.SearchRequest{Query: "zeppelin", Kinds: []core.HitKind{"gossip"}})
	if core.KindOf(err) != core.KindInvalid {
		t.Errorf("unknown kind: error %v, want invalid", err)
	}

	// Paging: three events hold one word, and the page size is two.
	for _, text := range []string{"quokka again", "quokka third"} {
		must(s.EventLog(ctx, core.EventLogRequest{TaskID: res.ID, Kind: core.EventNote, Text: text}))(t)
	}
	events := []core.HitKind{core.HitEvent}
	first := mustSearch(ctx, t, s, core.SearchRequest{Query: "quokka", Kinds: events, Limit: 2})
	if len(first.Hits) != 2 || first.Next == "" {
		t.Fatalf("first page = %d hits, next %q; want 2 and a cursor", len(first.Hits), first.Next)
	}
	second := mustSearch(ctx, t, s, core.SearchRequest{Query: "quokka", Kinds: events, Limit: 2, Cursor: first.Next})
	if len(second.Hits) != 1 || second.Next != "" {
		t.Errorf("second page = %d hits, next %q; want 1 and no cursor", len(second.Hits), second.Next)
	}
	seen := map[string]bool{}
	for _, h := range append(first.Hits, second.Hits...) {
		if seen[h.ID] {
			t.Errorf("hit %s on two pages", h.ID)
		}
		seen[h.ID] = true
	}
}

// TestSearchScopesByProject checks that another project's rows need
// all_projects, that a global directive is found from every project, and that
// punctuation in the query is text.
func TestSearchScopesByProject(t *testing.T) {
	s, st := openLocal(t)
	res := must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "t", Goal: "g"}))(t)
	must(s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventNote, Text: "quokka here"}))(t)
	insertDirective(t, st, "global", "", "", "Pangolin rule", "active")
	inB := as(projB, "main")

	if got := mustSearch(inB, t, s, core.SearchRequest{Query: "quokka"}); len(got.Hits) != 0 {
		t.Errorf("search in %s found %s rows: %+v", projB, projA, got.Hits)
	}
	if got := mustSearch(inB, t, s, core.SearchRequest{Query: "quokka", AllProjects: true}); len(got.Hits) == 0 {
		t.Errorf("all_projects search found nothing")
	}
	if got := mustSearch(inB, t, s, core.SearchRequest{Query: "pangolin"}); len(got.Hits) != 1 || got.Hits[0].ID != "D-0001" {
		t.Errorf("global directive from %s = %+v, want D-0001", projB, got.Hits)
	}
	mustSearch(inB, t, s, core.SearchRequest{Query: `"unbalanced AND (OR* -`})
}

// TestRecallAcrossProjects checks recall without close: the lesson, mistake and
// task come from another project, ranked by the goal, and the directive comes
// from the project's rules. A goal with no usable word gives directives only.
func TestRecallAcrossProjects(t *testing.T) {
	s, st := openLocal(t)
	ctxB := as(projB, "main")
	res := must(s.TaskStart(ctxB, core.TaskStartRequest{Title: "Migrate webhook signatures", Goal: "Verify webhook signatures with HMAC-SHA256."}))(t)
	seedLesson(t, st, res.ID, "Webhook bodies must be verified before JSON decoding.")
	seedMistake(t, st, res.ID, "Compared webhook signatures with ==")
	must(s.TaskStart(ctxB, core.TaskStartRequest{Title: "Unrelated chore", Goal: "Bump the linter.", Queue: true}))(t)
	insertDirective(t, st, "global", "", "", "Constant-time compare", "active")

	got := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "Add webhook signature checks to the API"})
	if len(got.Lessons) == 0 || got.Lessons[0].TaskID != res.ID || got.Lessons[0].Project != projB {
		t.Errorf("recall lessons = %+v, want the %s lesson from %s", got.Lessons, projB, res.ID)
	}
	if len(got.Mistakes) == 0 || got.Mistakes[0].TaskID != res.ID || got.Mistakes[0].Project != projB {
		t.Errorf("recall mistakes = %+v", got.Mistakes)
	}
	if len(got.Tasks) == 0 || got.Tasks[0].ID != res.ID {
		t.Errorf("recall tasks = %+v, want %s first", got.Tasks, res.ID)
	}
	for _, task := range got.Tasks {
		if task.Title == "Unrelated chore" {
			t.Errorf("recall returned an unrelated task")
		}
	}
	if titles := directiveTitles(got.Directives); !slices.Equal(titles, []string{"Constant-time compare"}) {
		t.Errorf("recall directives = %v", titles)
	}

	// Every word of this goal is a stop word or too short, so nothing matches.
	none := mustRecall(mainCtx(), t, s, core.RecallRequest{Goal: "the and with for"})
	if len(none.Lessons) != 0 || len(none.Tasks) != 0 || len(none.Directives) != 1 {
		t.Errorf("recall of a stop-word goal = %+v, want directives only", none)
	}
	_, err := s.Recall(mainCtx(), core.RecallRequest{Goal: "  "})
	if core.KindOf(err) != core.KindInvalid {
		t.Errorf("recall with a blank goal: error %v, want invalid", err)
	}
}
