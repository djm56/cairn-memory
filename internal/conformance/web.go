package conformance

import (
	"context"
	"slices"

	"github.com/djm56/cairn-memory/internal/core"
)

// viewer returns a viewer's context: no project and no agent, the viewer role. The web UI
// reads need neither.
func viewer() context.Context {
	return core.WithCaller(context.Background(), core.Caller{Role: core.RoleViewer})
}

// projectNamesOf returns the names of ps in order.
func projectNamesOf(ps []core.Project) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

// taskIDsOf returns the ids of ts in order.
func taskIDsOf(ts []core.TaskSummary) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

// webCases are the web UI's reads of CONTRACT §8.1 and item 50. The figures of the dashboard
// are core-only (item 51), since the suite cannot move the clock, so the dashboard here checks
// its role and its shape.
var webCases = []Case{
	{"web/reads_refuse_an_agent", func(t T, s core.Service) {
		// CONTRACT §8.1: an agent is refused with operator_only, whatever else the call says.
		_, err := s.ProjectList(mainCtx(), core.ProjectListRequest{})
		wantKind(t, err, core.KindOperatorOnly, "project_list by an agent")
		_, err = s.TaskBrowse(mainCtx(), core.TaskBrowseRequest{})
		wantKind(t, err, core.KindOperatorOnly, "task_browse by an agent")
		_, err = s.DirectiveBrowse(mainCtx(), core.DirectiveBrowseRequest{})
		wantKind(t, err, core.KindOperatorOnly, "directive_browse by an agent")
		_, err = s.Dashboard(mainCtx())
		wantKind(t, err, core.KindOperatorOnly, "dashboard by an agent")
	}},
	{"web/reads_need_no_project_and_make_no_row", func(t T, s core.Service) {
		// CONTRACT §8.1: an operator and a viewer read with no project, and no read creates a row.
		for _, ctx := range []context.Context{op(), viewer()} {
			must(s.ProjectList(ctx, core.ProjectListRequest{}))(t)
			must(s.TaskBrowse(ctx, core.TaskBrowseRequest{}))(t)
			must(s.DirectiveBrowse(ctx, core.DirectiveBrowseRequest{}))(t)
			must(s.Dashboard(ctx))(t)
		}
		if list := must(s.ProjectList(op(), core.ProjectListRequest{}))(t); len(list.Projects) != 0 {
			t.Errorf("project_list after the reads: %d projects, want 0", len(list.Projects))
		}
	}},
	{"web/project_list_orders_pages_and_filters", func(t T, s core.Service) {
		// CONTRACT §8.1: name order, a cursor, one project by its exact name, the settings
		// defaults of a new project, and the refusals.
		for _, name := range []string{"gamma", "alpha", "beta"} {
			must(s.TaskStart(as(name, "main"), core.TaskStartRequest{Title: "t", Goal: "g", Queue: true}))(t)
		}
		first := must(s.ProjectList(op(), core.ProjectListRequest{Limit: 2}))(t)
		if got := projectNamesOf(first.Projects); !slices.Equal(got, []string{"alpha", "beta"}) || first.Next == "" {
			t.Fatalf("first page = %v, next %q; want alpha and beta with a next cursor", got, first.Next)
		}
		second := must(s.ProjectList(op(), core.ProjectListRequest{Limit: 2, Cursor: first.Next}))(t)
		if got := projectNamesOf(second.Projects); !slices.Equal(got, []string{"gamma"}) || second.Next != "" {
			t.Errorf("second page = %v, next %q; want gamma and no next", got, second.Next)
		}
		one := must(s.ProjectList(viewer(), core.ProjectListRequest{Project: "beta"}))(t)
		if got := projectNamesOf(one.Projects); !slices.Equal(got, []string{"beta"}) {
			t.Errorf("project beta = %v, want only beta", got)
		} else if one.Projects[0].Settings != core.DefaultSettings() {
			t.Errorf("settings of a new project = %+v, want the defaults", one.Projects[0].Settings)
		}
		_, err := s.ProjectList(op(), core.ProjectListRequest{Project: "nobody"})
		wantKind(t, err, core.KindNotFound, "project_list of a name with no row")
		_, err = s.ProjectList(op(), core.ProjectListRequest{Project: "Not Valid"})
		wantKind(t, err, core.KindInvalid, "project_list of a bad name")
		_, err = s.ProjectList(op(), core.ProjectListRequest{Limit: -1})
		wantKind(t, err, core.KindInvalid, "project_list with a negative limit")
	}},
	{"web/task_browse_filters_and_pages", func(t T, s core.Service) {
		// CONTRACT §8.1: tasks of every project, most recently updated first with id descending on a
		// tie; a status filter; a project filter; the refusals.
		var ids []string
		for _, project := range []string{projA, projB, projA} {
			res := must(s.TaskStart(as(project, "main"), core.TaskStartRequest{Title: "t", Goal: "g", Queue: true}))(t)
			ids = append(ids, res.ID)
		}
		slices.Reverse(ids)
		page := must(s.TaskBrowse(op(), core.TaskBrowseRequest{Limit: 2}))(t)
		if got := taskIDsOf(page.Tasks); !slices.Equal(got, ids[:2]) || page.Next == "" {
			t.Fatalf("first page = %v, next %q; want %v with a next cursor", got, page.Next, ids[:2])
		}
		next := must(s.TaskBrowse(viewer(), core.TaskBrowseRequest{Limit: 2, Cursor: page.Next}))(t)
		if got := taskIDsOf(next.Tasks); !slices.Equal(got, ids[2:]) || next.Next != "" {
			t.Errorf("second page = %v, next %q; want %v and no next", got, next.Next, ids[2:])
		}
		web := must(s.TaskBrowse(op(), core.TaskBrowseRequest{Project: projB}))(t)
		if len(web.Tasks) != 1 || web.Tasks[0].Project != projB {
			t.Errorf("tasks of %s = %+v, want one task of that project", projB, web.Tasks)
		}
		open := must(s.TaskBrowse(op(), core.TaskBrowseRequest{Status: []core.TaskStatus{core.TaskOpen}}))(t)
		if len(open.Tasks) != 3 {
			t.Errorf("open tasks = %d, want 3", len(open.Tasks))
		}
		_, err := s.TaskBrowse(op(), core.TaskBrowseRequest{Project: "nobody"})
		wantKind(t, err, core.KindNotFound, "task_browse of a project with no row")
		_, err = s.TaskBrowse(op(), core.TaskBrowseRequest{Status: []core.TaskStatus{"sleeping"}})
		wantKind(t, err, core.KindInvalid, "task_browse with an unknown status")
	}},
	{"web/directive_browse_states", func(t T, s core.Service) {
		// CONTRACT §8.1: active by default, retired on request, nothing capped, the refusals.
		kept := must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "kept", Rule: "r"}))(t)
		gone := must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "gone", Rule: "r"}))(t)
		must(s.DirectiveRetire(op(), core.DirectiveRetireRequest{Code: gone.Code}))(t)
		active := must(s.DirectiveBrowse(viewer(), core.DirectiveBrowseRequest{}))(t)
		if len(active.Directives) != 1 || active.Directives[0].Code != kept.Code || active.Directives[0].Expired {
			t.Errorf("active directives = %+v, want only %s, not expired", active.Directives, kept.Code)
		}
		retired := must(s.DirectiveBrowse(op(), core.DirectiveBrowseRequest{State: core.DirectiveRetired}))(t)
		if len(retired.Directives) != 1 || retired.Directives[0].Code != gone.Code || retired.Directives[0].RetiredAt == nil {
			t.Errorf("retired directives = %+v, want only %s, with retired_at", retired.Directives, gone.Code)
		}
		_, err := s.DirectiveBrowse(mainCtx(), core.DirectiveBrowseRequest{})
		wantKind(t, err, core.KindOperatorOnly, "directive_browse by an agent")
		_, err = s.DirectiveBrowse(op(), core.DirectiveBrowseRequest{State: "pending"})
		wantKind(t, err, core.KindInvalid, "directive_browse of an unknown state")
	}},
	{"web/dashboard_shape", func(t T, s core.Service) {
		// CONTRACT §8.1: the shape of the dashboard. Its figures are core-only (item 51).
		must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "run", Goal: "g"}))(t)
		d := must(s.Dashboard(viewer()))(t)
		if d.Running.Count != 1 || len(d.Running.Tasks) != 1 {
			t.Errorf("running = %d, %d tasks; want 1 and 1", d.Running.Count, len(d.Running.Tasks))
		}
		if len(d.Monthly) != 6 {
			t.Fatalf("monthly = %d months, want 6", len(d.Monthly))
		}
		for i := 1; i < len(d.Monthly); i++ {
			if d.Monthly[i-1].Month >= d.Monthly[i].Month {
				t.Errorf("monthly not oldest first: %s before %s", d.Monthly[i-1].Month, d.Monthly[i].Month)
			}
		}
		if d.Directives.Active != 0 || d.Directives.OldestReviewBy != "" || d.Directives.OldestAgeDays != nil {
			t.Errorf("directives with none active = %+v, want no oldest figures", d.Directives)
		}
	}},
	{"web/learning_all_projects_for_readers", func(t T, s core.Service) {
		// CONTRACT §9 item 50: learning_list with all_projects needs no project for an operator or
		// a viewer, and an agent with no project is still refused.
		must(s.LearningAdd(as(projA, "main"), core.LearningAddRequest{Kind: core.LearningFinding, Title: "a", Body: "b"}))(t)
		must(s.LearningAdd(as(projB, "main"), core.LearningAddRequest{Kind: core.LearningFinding, Title: "b", Body: "b"}))(t)
		for _, ctx := range []context.Context{op(), viewer()} {
			got := must(s.LearningList(ctx, core.LearningListRequest{AllProjects: true}))(t)
			if len(got.Learnings) != 2 {
				t.Errorf("learning_list all_projects with no project: %d learnings, want 2", len(got.Learnings))
			}
		}
		if list := must(s.ProjectList(op(), core.ProjectListRequest{}))(t); len(list.Projects) != 2 {
			t.Errorf("projects after the reads = %v, want the two that agents wrote", projectNamesOf(list.Projects))
		}
		noProject := core.WithCaller(context.Background(), core.Caller{Agent: "main", Role: core.RoleAgent})
		_, err := s.LearningList(noProject, core.LearningListRequest{AllProjects: true})
		wantKind(t, err, core.KindInvalid, "learning_list all_projects by an agent with no project")
	}},
}
