package core_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// seedProject creates a project row, so a project directive can name it.
func seedProject(t *testing.T, st *store.Store, name string) {
	t.Helper()
	now := time.Now().UTC()
	err := store.EnsureProject(context.Background(), st, store.ProjectRow{
		Name: name, CreatedAt: now, LastSeenAt: now, Settings: `{"parallel_tasks":false}`,
	})
	if err != nil {
		t.Fatalf("seed project %s: %v", name, err)
	}
}

// insertDirective writes a directive row, with the next code, through the
// store, so the read and search tests do not depend on directive_ratify. A
// retired directive gets a retired_at time.
func insertDirective(t *testing.T, st *store.Store, scope, project, agent, title, status string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	row := store.DirectiveRow{
		Scope: scope, Project: project, Agent: agent, Title: title, Rule: "rule of " + title,
		Status: status, RatifiedBy: "operator", RatifiedAt: &now,
	}
	if status == "retired" {
		row.RetiredAt = &now
	}
	err := st.WithTx(ctx, func(q store.Querier) error {
		code, err := store.NextDirectiveCode(ctx, q)
		if err != nil {
			return err
		}
		row.Code = code
		_, err = store.InsertDirective(ctx, q, row)
		return err
	})
	if err != nil {
		t.Fatalf("insert directive %s: %v", title, err)
	}
}

// directiveTitles returns the titles of ds in order.
func directiveTitles(ds []core.Directive) []string {
	var out []string
	for _, d := range ds {
		out = append(out, d.Title)
	}
	return out
}

// TestDirectiveReadsByScope checks directive_list and task_current's directives
// against the scopes CONTRACT §5 names. The list is ordered by code, holds only
// active directives, and with an agent set narrows the agent-scoped ones to that
// role. task_current gives the project's directives with no task active too.
func TestDirectiveReadsByScope(t *testing.T) {
	s, st := openLocal(t)
	seedProject(t, st, "acme-api")
	seedProject(t, st, "acme-web")
	insertDirective(t, st, "global", "", "", "global", "active")
	insertDirective(t, st, "project", "acme-api", "", "project-a", "active")
	insertDirective(t, st, "project", "acme-web", "", "project-b", "active")
	insertDirective(t, st, "agent", "", "reviewer", "reviewer-only", "active")
	insertDirective(t, st, "global", "", "", "retired", "retired")
	ctx := agent("acme-api", "main")

	cases := []struct {
		name  string
		agent string
		want  []string
	}{
		{"no agent: every agent scope", "", []string{"global", "project-a", "reviewer-only"}},
		{"main: agent scopes narrowed away", "main", []string{"global", "project-a"}},
		{"reviewer: its own agent scope", "reviewer", []string{"global", "project-a", "reviewer-only"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := s.DirectiveList(ctx, core.DirectiveListRequest{Agent: c.agent})
			if err != nil {
				t.Fatal(err)
			}
			if titles := directiveTitles(got.Directives); !slices.Equal(titles, c.want) {
				t.Errorf("directive_list(agent %q) = %v, want %v", c.agent, titles, c.want)
			}
		})
	}

	// With no task active, task_current still answers with the project's
	// directives: no agent is set, so every agent scope is included.
	cur, err := s.TaskCurrent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Task != nil {
		t.Fatalf("TaskCurrent with no task = %s", cur.Task.ID)
	}
	if want := []string{"global", "project-a", "reviewer-only"}; !slices.Equal(directiveTitles(cur.Directives), want) {
		t.Errorf("TaskCurrent directives = %v, want %v", directiveTitles(cur.Directives), want)
	}
}

// TestLearningListFilters checks learning_list's filters and paging. A status
// other than proposed is set through the store here, so the test does not
// depend on learning_decide.
func TestLearningListFilters(t *testing.T) {
	s, st := openLocal(t)
	seedProject(t, st, "acme-web")
	ctx := agent("acme-api", "main")
	a := mustLearning(ctx, t, s, core.LearningAddRequest{Kind: core.LearningFinding, Title: "a", Body: "b", Target: "reviewer"})
	b := mustLearning(ctx, t, s, core.LearningAddRequest{Kind: core.LearningPattern, Title: "b", Body: "b"})
	c := mustLearning(agent("acme-web", "main"), t, s, core.LearningAddRequest{Kind: core.LearningFinding, Title: "c", Body: "b"})

	// Mark b accepted, the way a later decision would.
	row, err := store.GetLearningAnyProject(context.Background(), st, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	row.Status = string(core.LearningAccepted)
	if err := st.WithTx(context.Background(), func(q store.Querier) error {
		return store.UpdateLearning(context.Background(), q, row)
	}); err != nil {
		t.Fatal(err)
	}

	ids := func(ls []core.Learning) []int64 {
		var out []int64
		for _, l := range ls {
			out = append(out, l.ID)
		}
		return out
	}
	list := func(req core.LearningListRequest) []int64 {
		t.Helper()
		got, err := s.LearningList(ctx, req)
		if err != nil {
			t.Fatalf("LearningList(%+v): %v", req, err)
		}
		return ids(got.Learnings)
	}
	if got := list(core.LearningListRequest{}); !slices.Equal(got, []int64{b.ID, a.ID}) {
		t.Errorf("own project, newest first = %v, want [%d %d]", got, b.ID, a.ID)
	}
	if got := list(core.LearningListRequest{Status: []core.LearningStatus{core.LearningProposed}}); !slices.Equal(got, []int64{a.ID}) {
		t.Errorf("proposed = %v, want [%d]", got, a.ID)
	}
	if got := list(core.LearningListRequest{AllProjects: true, Kind: []core.LearningKind{core.LearningPattern}}); !slices.Equal(got, []int64{b.ID}) {
		t.Errorf("patterns across projects = %v, want [%d]", got, b.ID)
	}
	if got := list(core.LearningListRequest{Target: "reviewer"}); !slices.Equal(got, []int64{a.ID}) {
		t.Errorf("target reviewer = %v, want [%d]", got, a.ID)
	}
	if got := list(core.LearningListRequest{AllProjects: true}); !slices.Equal(got, []int64{c.ID, b.ID, a.ID}) {
		t.Errorf("all projects = %v, want [%d %d %d]", got, c.ID, b.ID, a.ID)
	}

	// Paging one at a time walks the three rows and then stops.
	var walked []int64
	cursor := ""
	for pages := 0; pages < 5; pages++ {
		got, err := s.LearningList(ctx, core.LearningListRequest{AllProjects: true, Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		walked = append(walked, ids(got.Learnings)...)
		if got.Next == "" {
			break
		}
		cursor = got.Next
	}
	if !slices.Equal(walked, []int64{c.ID, b.ID, a.ID}) {
		t.Errorf("paged walk = %v, want [%d %d %d]", walked, c.ID, b.ID, a.ID)
	}

	for name, req := range map[string]core.LearningListRequest{
		"unknown status":  {Status: []core.LearningStatus{"gossip"}},
		"unknown kind":    {Kind: []core.LearningKind{"rumour"}},
		"bad cursor":      {Cursor: "!!!"},
		"limit too large": {Limit: core.MaxLimit + 1},
	} {
		_, err := s.LearningList(ctx, req)
		if core.KindOf(err) != core.KindInvalid {
			t.Errorf("%s: error %v, want invalid", name, err)
		}
	}
}

// mustLearning adds a learning and fails the test on error.
func mustLearning(ctx context.Context, t *testing.T, s *core.Local, req core.LearningAddRequest) *core.Learning {
	t.Helper()
	l, err := s.LearningAdd(ctx, req)
	if err != nil {
		t.Fatalf("LearningAdd: %v", err)
	}
	return l
}
