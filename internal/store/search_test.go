package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// seedSearch writes one row of each searchable kind into two projects, all
// containing the word "tests". Two directives have no project: a global one and
// an agent one. The rest are scoped to acme-api or other.
func seedSearch(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	seedProjects(t, s, "acme-api", "other")
	insertTask(t, s, TaskRow{ID: "task-20261028-01", Project: "acme-api", Title: "acme tests",
		Goal: "goal", Status: "open", Priority: "normal", OpenedAt: t0, UpdatedAt: t0})
	insertTask(t, s, TaskRow{ID: "task-20261028-02", Project: "other", Title: "other tests",
		Goal: "goal", Status: "open", Priority: "normal", OpenedAt: t0, UpdatedAt: t0})

	if err := s.WithTx(ctx, func(q Querier) error {
		for _, p := range []struct{ task, project string }{
			{"task-20261028-01", "acme-api"}, {"task-20261028-02", "other"},
		} {
			if _, err := InsertEvent(ctx, q, EventRow{TaskID: p.task, Project: p.project,
				Agent: "main", Kind: "note", Text: "tests ran", OccurredAt: t0}); err != nil {
				return err
			}
		}
		for _, p := range []string{"acme-api", "other"} {
			if _, err := InsertLearning(ctx, q, LearningRow{Project: p, Agent: "main",
				Kind: "finding", Title: "tests learning", Body: "b", Status: "proposed", CreatedAt: t0}); err != nil {
				return err
			}
		}
		if err := ReplaceLessons(ctx, q, "task-20261028-01", []LessonRow{
			{TaskID: "task-20261028-01", Ord: 1, Kind: "reuse", Text: "tests lesson"},
		}); err != nil {
			return err
		}
		directives := []DirectiveRow{
			{Scope: "global", Title: "tests global", Rule: "r"},
			{Scope: "project", Project: "acme-api", Title: "tests acme only", Rule: "r"},
			{Scope: "project", Project: "other", Title: "tests other only", Rule: "r"},
			{Scope: "agent", Agent: "hicks", Title: "tests for hicks", Rule: "r"},
		}
		for _, d := range directives {
			code, err := NextDirectiveCode(ctx, q)
			if err != nil {
				return err
			}
			d.Code = code
			d.Status = "active"
			if _, err := InsertDirective(ctx, q, d); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed search rows: %v", err)
	}
}

// countByKindProject counts search rows as "kind/project", so a test can say
// which rows came back without knowing their ids. A directive with no project
// counts as "kind/".
func countByKindProject(rows []SearchRow) map[string]int {
	out := map[string]int{}
	for _, r := range rows {
		out[r.Kind+"/"+r.Project]++
	}
	return out
}

// TestSearchScoping checks the project scope and the all_projects switch. A
// project-less directive (global or agent-scoped) shows in every project, and
// another project's rows show only when AllProjects is set.
func TestSearchScoping(t *testing.T) {
	s := openTemp(t)
	seedSearch(t, s)
	ctx := context.Background()

	cases := []struct {
		name   string
		filter SearchFilter
		want   map[string]int
	}{
		{"acme scope", SearchFilter{Query: "tests", Project: "acme-api", Limit: 50},
			map[string]int{
				"task/acme-api": 1, "event/acme-api": 1, "learning/acme-api": 1,
				"lesson/acme-api": 1, "directive/acme-api": 1, "directive/": 2,
			}},
		{"other scope", SearchFilter{Query: "tests", Project: "other", Limit: 50},
			map[string]int{
				"task/other": 1, "event/other": 1, "learning/other": 1,
				"directive/other": 1, "directive/": 2,
			}},
		{"all projects", SearchFilter{Query: "tests", Project: "acme-api", AllProjects: true, Limit: 50},
			map[string]int{
				"task/acme-api": 1, "task/other": 1, "event/acme-api": 1, "event/other": 1,
				"learning/acme-api": 1, "learning/other": 1, "lesson/acme-api": 1,
				"directive/acme-api": 1, "directive/other": 1, "directive/": 2,
			}},
		{"kinds narrow", SearchFilter{Query: "tests", Project: "acme-api", Kinds: []string{"directive"}, Limit: 50},
			map[string]int{"directive/acme-api": 1, "directive/": 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, more, err := Search(ctx, s, c.filter)
			if err != nil || more {
				t.Fatalf("Search err %v, more %v", err, more)
			}
			got := countByKindProject(rows)
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("rows = %v, want %v", got, c.want)
			}
		})
	}
}

// TestSearchHitColumns checks that each hit carries the project and task id its
// row was indexed with, and that the snippet keeps the matched word in brackets.
func TestSearchHitColumns(t *testing.T) {
	s := openTemp(t)
	seedSearch(t, s)
	rows, _, err := Search(context.Background(), s, SearchFilter{Query: "tests", Project: "acme-api", Limit: 50})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	var sawTask, sawLesson bool
	for _, r := range rows {
		if r.Kind == "task" {
			sawTask = true
			if r.TaskID != "task-20261028-01" || r.Ref != "task-20261028-01" {
				t.Errorf("task hit task id %q ref %q", r.TaskID, r.Ref)
			}
		}
		if r.Kind == "lesson" {
			sawLesson = true
			if r.Ref != "task-20261028-01/1" || r.TaskID != "task-20261028-01" || r.Project != "acme-api" {
				t.Errorf("lesson hit = %+v", r)
			}
			task, ord, err := ParseChildRef(r.Ref)
			if err != nil || task != r.TaskID || ord != 1 {
				t.Errorf("lesson ref %q parses to %q %d %v", r.Ref, task, ord, err)
			}
		}
		if r.Kind == "task" && !strings.Contains(r.Snippet, "[tests]") {
			t.Errorf("task snippet %q has no bracketed match", r.Snippet)
		}
	}
	if !sawTask || !sawLesson {
		t.Errorf("missing hits: task %v, lesson %v", sawTask, sawLesson)
	}
}

// TestSearchPaging walks every hit in pages of three and checks that together
// they are all of them, once each.
func TestSearchPaging(t *testing.T) {
	s := openTemp(t)
	seedSearch(t, s)
	ctx := context.Background()
	all, _, err := Search(ctx, s, SearchFilter{Query: "tests", Project: "acme-api", Limit: 50})
	if err != nil {
		t.Fatalf("Search all: %v", err)
	}
	seen := map[string]bool{}
	var walked int
	for offset := 0; ; offset += 3 {
		rows, more, err := Search(ctx, s, SearchFilter{Query: "tests", Project: "acme-api", Offset: offset, Limit: 3})
		if err != nil {
			t.Fatalf("page at %d: %v", offset, err)
		}
		for _, r := range rows {
			key := r.Kind + "|" + r.Ref
			if seen[key] {
				t.Errorf("hit %s returned twice", key)
			}
			seen[key] = true
			walked++
		}
		if !more {
			break
		}
	}
	if walked != len(all) {
		t.Errorf("walked %d hits, want %d", walked, len(all))
	}
}

// TestSearchPlainText checks that query syntax is matched as words and never
// raises an error: quotes, operators, brackets and prefix marks.
func TestSearchPlainText(t *testing.T) {
	s := openTemp(t)
	seedSearch(t, s)
	ctx := context.Background()
	queries := []string{
		`tests"`, `"tests`, `tests OR`, `NOT tests`, `tests*`, `(tests`, `-tests`,
		`title:tests`, `NEAR(tests)`, `"`, `""`, `*`, `"""`, `tests AND (`,
	}
	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			if _, _, err := Search(ctx, s, SearchFilter{Query: q, Project: "acme-api", Limit: 10}); err != nil {
				t.Errorf("Search(%q): %v", q, err)
			}
		})
	}

	// Each word is one phrase, and the quote, bracket, star and dash are
	// separators to the tokenizer, so a word that carries them still finds its
	// text. The seed holds seven acme-api rows with "tests" (the acme case of
	// TestSearchScoping). NOT is a word too, ANDed with tests, and no row holds
	// it, so NOT tests finds none (CONTRACT §6: operators are matched as words).
	found := []struct {
		query string
		want  int
	}{
		{`"tests`, 7}, {`tests"`, 7}, {`(tests`, 7}, {`tests*`, 7}, {`-tests`, 7},
		{`NOT tests`, 0},
	}
	for _, c := range found {
		t.Run("hits "+c.query, func(t *testing.T) {
			rows, _, err := Search(ctx, s, SearchFilter{Query: c.query, Project: "acme-api", Limit: 50})
			if err != nil || len(rows) != c.want {
				t.Errorf("Search(%q) = %d rows, err %v; want %d", c.query, len(rows), err, c.want)
			}
		})
	}

	rows, _, err := Search(ctx, s, SearchFilter{Query: `title:tests`, Project: "acme-api", Limit: 50})
	if err != nil || len(rows) != 0 {
		t.Errorf("title:tests = %d rows, err %v; want none (the colon is a word, not a column filter)", len(rows), err)
	}
	rows, _, err = Search(ctx, s, SearchFilter{Query: "   ", Project: "acme-api", Limit: 10})
	if err != nil || len(rows) != 0 {
		t.Errorf("blank query = %d rows, err %v; want none", len(rows), err)
	}
}

// TestSearchPlainTextMirror backs the zero-hit checks in TestSearchPlainText. It
// adds one acme-api row that holds the words of `NOT tests` and the phrase
// `title:tests`, and each query must find that row. Each word of a query is a
// phrase, and `title:tests` is one phrase, so "title" and "tests" must sit next
// to each other in the row, in that order. Without the row, zero hits could mean
// the search is broken rather than that no row matches.
func TestSearchPlainTextMirror(t *testing.T) {
	s := openTemp(t)
	seedSearch(t, s)
	ctx := context.Background()
	if err := s.WithTx(ctx, func(q Querier) error {
		_, err := InsertEvent(ctx, q, EventRow{TaskID: "task-20261028-01", Project: "acme-api",
			Agent: "main", Kind: "note", Text: "title tests not", OccurredAt: t0})
		return err
	}); err != nil {
		t.Fatalf("insert mirror event: %v", err)
	}
	for _, query := range []string{`NOT tests`, `title:tests`} {
		rows, _, err := Search(ctx, s, SearchFilter{Query: query, Project: "acme-api", Limit: 50})
		if err != nil || len(rows) != 1 {
			t.Errorf("Search(%q) = %d rows, err %v; want the one mirror row", query, len(rows), err)
		}
	}
}

// TestSearchArguments checks the two argument errors: a limit below 1 and a
// negative offset.
func TestSearchArguments(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	if _, _, err := Search(ctx, s, SearchFilter{Query: "x", Project: "acme-api"}); err == nil {
		t.Error("limit 0 accepted")
	}
	if _, _, err := Search(ctx, s, SearchFilter{Query: "x", Project: "acme-api", Limit: 1, Offset: -1}); err == nil {
		t.Error("negative offset accepted")
	}
}
