package core_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// viewerCtx is a viewer's context: no project, no agent, the viewer role.
func viewerCtx() context.Context {
	return core.WithCaller(context.Background(), core.Caller{Role: core.RoleViewer})
}

// webReads are the four web UI reads of CONTRACT §8.1, each with a request that is valid on
// its own, so the role check is what answers.
var webReads = []struct {
	name string
	call func(context.Context, core.Service) error
}{
	{"project_list", func(ctx context.Context, s core.Service) error {
		_, err := s.ProjectList(ctx, core.ProjectListRequest{})
		return err
	}},
	{"task_browse", func(ctx context.Context, s core.Service) error {
		_, err := s.TaskBrowse(ctx, core.TaskBrowseRequest{})
		return err
	}},
	{"directive_browse", func(ctx context.Context, s core.Service) error {
		_, err := s.DirectiveBrowse(ctx, core.DirectiveBrowseRequest{})
		return err
	}},
	{"dashboard", func(ctx context.Context, s core.Service) error {
		_, err := s.Dashboard(ctx)
		return err
	}},
}

// TestWebReadsRefuseAnAgent checks CONTRACT §8.1: an agent caller, or a caller with no role,
// is refused with operator_only before anything else, so a bad request still gets that answer.
func TestWebReadsRefuseAnAgent(t *testing.T) {
	s, st := openLocal(t)
	callers := map[string]context.Context{
		"agent":   agent("acme-api", "main"),
		"no role": core.WithCaller(context.Background(), core.Caller{Project: "acme-api", Agent: "main"}),
		"none":    context.Background(),
	}
	for _, r := range webReads {
		for who, ctx := range callers {
			err := r.call(ctx, s)
			wantKindLocal(t, err, core.KindOperatorOnly, r.name+" by "+who)
		}
	}
	// The refusal writes nothing and reads nothing.
	if _, err := store.GetProject(context.Background(), st, "acme-api"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a refused agent read created a project row: %v", err)
	}
}

// TestWebReadsNeedNoProjectAndCreateNoRow checks CONTRACT §8.1 and §9 item 50: an operator and a
// viewer may make each read with no project, and none of them creates a project row. A
// project_list after the reads shows no project, so nothing was written.
func TestWebReadsNeedNoProjectAndCreateNoRow(t *testing.T) {
	s, _ := openLocal(t)
	for _, r := range webReads {
		if err := r.call(op(), s); err != nil {
			t.Errorf("%s by an operator with no project: %v", r.name, err)
		}
		if err := r.call(viewerCtx(), s); err != nil {
			t.Errorf("%s by a viewer with no project: %v", r.name, err)
		}
	}
	list := must(s.ProjectList(op(), core.ProjectListRequest{}))(t)
	if len(list.Projects) != 0 {
		t.Errorf("project_list after the reads = %d projects, want 0: a read created a row", len(list.Projects))
	}
}

// op is the operator's context, the one opCtx gives, under the short name the reads below use.
func op() context.Context { return opCtx() }

// TestProjectListPagesByNameAndFilters checks project_list: name order, paging with a cursor, one
// project by its exact name, not_found for a name with no row, invalid for a bad name or cursor.
func TestProjectListPagesByNameAndFilters(t *testing.T) {
	s, _ := openLocal(t)
	for _, name := range []string{"gamma", "alpha", "beta"} {
		must(s.TaskStart(agent(name, "main"), core.TaskStartRequest{Title: "t", Goal: "g", Queue: true}))(t)
	}

	first := must(s.ProjectList(op(), core.ProjectListRequest{Limit: 2}))(t)
	if got := projectNames(first.Projects); !slices.Equal(got, []string{"alpha", "beta"}) || first.Next == "" {
		t.Fatalf("first page = %v, next %q; want alpha and beta with a next cursor", got, first.Next)
	}
	second := must(s.ProjectList(op(), core.ProjectListRequest{Limit: 2, Cursor: first.Next}))(t)
	if got := projectNames(second.Projects); !slices.Equal(got, []string{"gamma"}) || second.Next != "" {
		t.Errorf("second page = %v, next %q; want gamma and no next", got, second.Next)
	}

	one := must(s.ProjectList(viewerCtx(), core.ProjectListRequest{Project: "beta"}))(t)
	if got := projectNames(one.Projects); !slices.Equal(got, []string{"beta"}) {
		t.Errorf("project beta = %v, want only beta", got)
	}
	if one.Projects[0].Settings != core.DefaultSettings() {
		t.Errorf("settings of a new project = %+v, want the defaults", one.Projects[0].Settings)
	}

	_, err := s.ProjectList(op(), core.ProjectListRequest{Project: "nope"})
	wantKindLocal(t, err, core.KindNotFound, "project_list of a name with no row")
	_, err = s.ProjectList(op(), core.ProjectListRequest{Project: "Not Valid"})
	wantKindLocal(t, err, core.KindInvalid, "project_list of a bad name")
	_, err = s.ProjectList(op(), core.ProjectListRequest{Cursor: "%%%"})
	wantKindLocal(t, err, core.KindInvalid, "project_list with a bad cursor")
	_, err = s.ProjectList(op(), core.ProjectListRequest{Limit: 101})
	wantKindLocal(t, err, core.KindInvalid, "project_list over the limit")
}

// TestProjectListDefaultsAnEmptySettingsObject checks CONTRACT §8.1: a stored settings value of
// '{}', the column default, reads as the defaults of DefaultSettings.
func TestProjectListDefaultsAnEmptySettingsObject(t *testing.T) {
	s, st := openLocal(t)
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if err := store.EnsureProject(context.Background(), st, store.ProjectRow{
		Name: "raw", CreatedAt: at, LastSeenAt: at, Settings: "{}",
	}); err != nil {
		t.Fatal(err)
	}
	got := must(s.ProjectList(op(), core.ProjectListRequest{Project: "raw"}))(t)
	if got.Projects[0].Settings != core.DefaultSettings() {
		t.Errorf("settings of '{}' = %+v, want %+v", got.Projects[0].Settings, core.DefaultSettings())
	}
}

// TestTaskBrowseAcrossProjects checks task_browse: tasks of every project, most recently
// updated first with id descending on a tie, paged; a status filter; a project filter; and the
// refusals. The clock is held still, so every task has one updated_at and the id is what orders them.
func TestTaskBrowseAcrossProjects(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	var ids []string
	for i, project := range []string{"acme-api", "acme-web", "acme-api", "acme-web", "acme-api"} {
		res := must(s.TaskStart(agent(project, "main"), core.TaskStartRequest{
			Title: fmt.Sprintf("task %d", i), Goal: "g", Queue: true,
		}))(t)
		ids = append(ids, res.ID)
	}
	slices.Reverse(ids) // id descending

	page := must(s.TaskBrowse(viewerCtx(), core.TaskBrowseRequest{Limit: 2}))(t)
	if got := taskIDs(page.Tasks); !slices.Equal(got, ids[:2]) || page.Next == "" {
		t.Fatalf("first page = %v, next %q; want %v with a next cursor", got, page.Next, ids[:2])
	}
	next := must(s.TaskBrowse(viewerCtx(), core.TaskBrowseRequest{Limit: 2, Cursor: page.Next}))(t)
	if got := taskIDs(next.Tasks); !slices.Equal(got, ids[2:4]) {
		t.Errorf("second page = %v, want %v", got, ids[2:4])
	}
	last := must(s.TaskBrowse(viewerCtx(), core.TaskBrowseRequest{Limit: 2, Cursor: next.Next}))(t)
	if got := taskIDs(last.Tasks); !slices.Equal(got, ids[4:]) || last.Next != "" {
		t.Errorf("last page = %v, next %q; want %v and no next", got, last.Next, ids[4:])
	}

	byProject := must(s.TaskBrowse(op(), core.TaskBrowseRequest{Project: "acme-web"}))(t)
	if len(byProject.Tasks) != 2 {
		t.Errorf("acme-web tasks = %d, want 2", len(byProject.Tasks))
	}
	for _, task := range byProject.Tasks {
		if task.Project != "acme-web" {
			t.Errorf("task %s of project %s listed under acme-web", task.ID, task.Project)
		}
	}

	queued := must(s.TaskBrowse(op(), core.TaskBrowseRequest{Status: []core.TaskStatus{core.TaskOpen}}))(t)
	if len(queued.Tasks) != 5 {
		t.Errorf("open tasks = %d, want 5", len(queued.Tasks))
	}
	active := must(s.TaskBrowse(op(), core.TaskBrowseRequest{Status: []core.TaskStatus{core.TaskActive}}))(t)
	if len(active.Tasks) != 0 || active.Tasks == nil {
		t.Errorf("active tasks = %v, want an empty list", active.Tasks)
	}

	_, err := s.TaskBrowse(op(), core.TaskBrowseRequest{Project: "nobody"})
	wantKindLocal(t, err, core.KindNotFound, "task_browse of a project with no row")
	_, err = s.TaskBrowse(op(), core.TaskBrowseRequest{Status: []core.TaskStatus{"sleeping"}})
	wantKindLocal(t, err, core.KindInvalid, "task_browse with an unknown status")
}

// TestDirectiveBrowseOrdersAndFlagsExpired checks directive_browse: active by review_by then
// code, with an expired one listed and flagged; retired by retired_at descending; no cap at 12;
// an unknown state is invalid.
func TestDirectiveBrowseOrdersAndFlagsExpired(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	// Fourteen active directives: more than the cap of the other reads, so the no-cap rule shows.
	for i := 0; i < 14; i++ {
		must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{
			Scope: core.ScopeGlobal, Title: fmt.Sprintf("d%02d", i), Rule: "r",
			ReviewBy: "2027-02-01",
		}))(t)
	}
	// One directive due before the clock reads, and one retired.
	clockAt(s, t0)
	must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "due", Rule: "r", ReviewBy: "2026-11-10"}))(t)
	retired := must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "gone", Rule: "r"}))(t)
	clockAt(s, t0.Add(time.Hour))
	must(s.DirectiveRetire(opCtx(), core.DirectiveRetireRequest{Code: retired.Code}))(t)

	clockAt(s, time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC))
	active := must(s.DirectiveBrowse(viewerCtx(), core.DirectiveBrowseRequest{Limit: 100}))(t)
	if len(active.Directives) != 15 {
		t.Fatalf("active directives = %d, want all 15: nothing is capped", len(active.Directives))
	}
	if first := active.Directives[0]; first.Title != "due" || !first.Expired {
		t.Errorf("first active = %q expired %v, want the due directive, expired", first.Title, first.Expired)
	}
	if second := active.Directives[1]; second.Title != "d00" || second.Expired {
		t.Errorf("second active = %q expired %v, want d00, live (the review_by ties, so the code orders)", second.Title, second.Expired)
	}

	gone := must(s.DirectiveBrowse(viewerCtx(), core.DirectiveBrowseRequest{State: core.DirectiveRetired}))(t)
	if len(gone.Directives) != 1 || gone.Directives[0].Title != "gone" || gone.Directives[0].RetiredAt == nil {
		t.Errorf("retired = %+v, want the one retired directive with retired_at", gone.Directives)
	}

	_, err := s.DirectiveBrowse(viewerCtx(), core.DirectiveBrowseRequest{State: "pending"})
	wantKindLocal(t, err, core.KindInvalid, "directive_browse of an unknown state")
}

// TestDirectiveBrowsePagesActiveByReviewBy checks the keyset of directive_browse: the second page
// starts after the last row of the first, even across a run of equal review_by days.
func TestDirectiveBrowsePagesActiveByReviewBy(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	var codes []string
	for i := 0; i < 5; i++ {
		d := must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{
			Scope: core.ScopeGlobal, Title: fmt.Sprintf("p%d", i), Rule: "r", ReviewBy: "2027-03-01",
		}))(t)
		codes = append(codes, d.Code)
	}
	var got []string
	cursor := ""
	for {
		page := must(s.DirectiveBrowse(viewerCtx(), core.DirectiveBrowseRequest{Limit: 2, Cursor: cursor}))(t)
		for _, d := range page.Directives {
			got = append(got, d.Code)
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if !slices.Equal(got, codes) {
		t.Errorf("paged codes = %v, want %v once each, in code order", got, codes)
	}
}

// TestDirectiveBrowseRetiredPagesInOrder checks step 6 review W3: retired directives are ordered
// by retired_at descending, then code ascending, and a page that ends inside a tie on retired_at
// resumes at the next code. Two directives share one retired_at, and the third is later.
func TestDirectiveBrowseRetiredPagesInOrder(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	d1 := ratifyGlobal(t, s, "first")
	d2 := ratifyGlobal(t, s, "second")
	d3 := ratifyGlobal(t, s, "third")
	early, late := t0.Add(time.Hour), t0.Add(2*time.Hour)
	clockAt(s, early)
	must(s.DirectiveRetire(opCtx(), core.DirectiveRetireRequest{Code: d1.Code}))(t)
	must(s.DirectiveRetire(opCtx(), core.DirectiveRetireRequest{Code: d3.Code}))(t)
	clockAt(s, late)
	must(s.DirectiveRetire(opCtx(), core.DirectiveRetireRequest{Code: d2.Code}))(t)

	// The later retirement comes first, then the tie in code order: d2, then d1 and d3.
	want := []string{d2.Code, d1.Code, d3.Code}
	var got []string
	cursor := ""
	for pages := 0; pages < 10; pages++ {
		page := must(s.DirectiveBrowse(viewerCtx(), core.DirectiveBrowseRequest{
			State: core.DirectiveRetired, Limit: 1, Cursor: cursor,
		}))(t)
		for _, d := range page.Directives {
			if d.Expired {
				t.Errorf("retired directive %s flagged expired", d.Code)
			}
			got = append(got, d.Code)
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if !slices.Equal(got, want) {
		t.Errorf("retired codes paged one at a time = %v, want %v", got, want)
	}
}

// TestExpiryAtReviewByAgreesAcrossTheReads checks step 6 review W3: a directive whose review_by is
// exactly now is live, and one a nanosecond later is expired. directive_list, the dashboard's
// expired figure and directive_browse's flag must say the same at both instants. The three read
// the rule in different code (Go in directive_list and directive_browse, SQL in the dashboard), so
// a change to one of them alone fails here.
func TestExpiryAtReviewByAgreesAcrossTheReads(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{
		Scope: core.ScopeGlobal, Title: "due", Rule: "r", ReviewBy: "2026-11-10",
	}))(t)
	due := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		label   string
		at      time.Time
		expired bool
	}{
		{"at review_by", due, false},
		{"a nanosecond after review_by", due.Add(time.Nanosecond), true},
	}
	for _, c := range cases {
		clockAt(s, c.at)
		list := must(s.DirectiveList(agent("acme-api", "main"), core.DirectiveListRequest{}))(t)
		if got := list.Expired == 1; got != c.expired {
			t.Errorf("%s: directive_list expired = %d, want expired %v", c.label, list.Expired, c.expired)
		}
		if got := len(list.Directives) == 0; got != c.expired {
			t.Errorf("%s: directive_list lists %d, want the directive left out %v", c.label, len(list.Directives), c.expired)
		}

		dash := must(s.Dashboard(viewerCtx()))(t)
		if got := dash.Waiting.ExpiredDirectives == 1; got != c.expired {
			t.Errorf("%s: dashboard expired_directives = %d, want expired %v", c.label, dash.Waiting.ExpiredDirectives, c.expired)
		}

		browse := must(s.DirectiveBrowse(viewerCtx(), core.DirectiveBrowseRequest{}))(t)
		if len(browse.Directives) != 1 || browse.Directives[0].Expired != c.expired {
			t.Errorf("%s: directive_browse = %+v, want one directive with expired %v", c.label, browse.Directives, c.expired)
		}
	}
}

// TestLearningListAllProjectsForReaders checks CONTRACT §9 item 50 for learning_list: with
// AllProjects, an operator or a viewer with no project reads every project's learnings and
// creates no row. An operator with a project and AllProjects creates none either. The other
// callers are read as before: an agent with no project is no_project, and an operator without
// AllProjects and with no project is no_project too.
func TestLearningListAllProjectsForReaders(t *testing.T) {
	s, st := openLocal(t)
	mustLearning(agent("acme-api", "main"), t, s, core.LearningAddRequest{Kind: core.LearningFinding, Title: "a", Body: "b"})
	mustLearning(agent("acme-web", "main"), t, s, core.LearningAddRequest{Kind: core.LearningFinding, Title: "w", Body: "b"})

	for who, ctx := range map[string]context.Context{"operator": op(), "viewer": viewerCtx()} {
		got := must(s.LearningList(ctx, core.LearningListRequest{AllProjects: true}))(t)
		if len(got.Learnings) != 2 {
			t.Errorf("%s with all_projects and no project: %d learnings, want 2 from both projects", who, len(got.Learnings))
		}
	}
	got := must(s.LearningList(core.WithCaller(context.Background(), core.Caller{Role: core.RoleOperator, Agent: "operator", Project: "fresh-ops"}),
		core.LearningListRequest{AllProjects: true}))(t)
	if len(got.Learnings) != 2 {
		t.Errorf("operator with a project and all_projects: %d learnings, want 2", len(got.Learnings))
	}
	if _, err := store.GetProject(context.Background(), st, "fresh-ops"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("operator all_projects read created the project row for its named project: %v", err)
	}

	// ErrNoProject is an invalid error: its message names cairn init.
	_, err := s.LearningList(context.Background(), core.LearningListRequest{AllProjects: true})
	wantKindLocal(t, err, core.KindInvalid, "all_projects read with no caller")
	_, err = s.LearningList(core.WithCaller(context.Background(), core.Caller{Role: core.RoleAgent, Agent: "main"}),
		core.LearningListRequest{AllProjects: true})
	wantKindLocal(t, err, core.KindInvalid, "agent all_projects read with no project")
	_, err = s.LearningList(core.WithCaller(context.Background(), core.Caller{Role: core.RoleOperator, Agent: "operator"}),
		core.LearningListRequest{})
	wantKindLocal(t, err, core.KindInvalid, "operator read without all_projects and with no project")

	// The agent read with all_projects and a project still lists every project, as it always has.
	agentAll := must(s.LearningList(agent("acme-api", "main"), core.LearningListRequest{AllProjects: true}))(t)
	if len(agentAll.Learnings) != 2 {
		t.Errorf("agent all_projects read: %d learnings, want 2 as before", len(agentAll.Learnings))
	}
}

// TestDashboardFigures checks each figure of CONTRACT §8.1 and §9 item 51 against a set of rows
// placed at the edges of the windows. "Now" is held at N by the clock seam, and every row is
// written at the time its figure needs. The expected values are worked out by hand in the
// comments of each block.
func TestDashboardFigures(t *testing.T) {
	s, _ := openLocal(t)
	n := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	ns := time.Nanosecond
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

	// Quality window [N-30d, N) = [Oct 16 12:00, Nov 15 12:00). The first close is exactly 30 days
	// before N and counts; the one a nanosecond earlier does not. The one at N does not count.
	fromWindow := n.AddDate(0, 0, -30)
	high := []core.Mistake{{Agent: "main", Mistake: "m", Impact: core.ImpactHigh}}
	med := []core.Mistake{{Agent: "main", Mistake: "m", Impact: core.ImpactMed}}
	met := core.CriterionOutcome{Ord: 1, Result: core.CriterionMet, Evidence: "e"}
	unmet := core.CriterionOutcome{Ord: 2, Result: core.CriterionUnmet, Evidence: "e"}

	closeTaskAt(t, s, "acme-api", day(2026, 6, 1), core.TaskDone, nil, nil)                              // June: monthly only
	closeTaskAt(t, s, "acme-api", day(2026, 6, 1).Add(-ns), core.TaskDone, nil, nil)                     // May: no figure
	closeTaskAt(t, s, "acme-api", fromWindow, core.TaskDone, []core.CriterionOutcome{met, unmet}, high)  // in both windows
	closeTaskAt(t, s, "acme-api", fromWindow.Add(-ns), core.TaskDone, []core.CriterionOutcome{met}, nil) // before the window
	closeTaskAt(t, s, "acme-api", day(2026, 11, 1).Add(-ns), core.TaskDone, nil, nil)                    // October's last instant
	closeTaskAt(t, s, "acme-api", day(2026, 11, 1), core.TaskDone, nil, nil)                             // November's first instant
	closeTaskAt(t, s, "acme-api", n.Add(-ns), core.TaskFailed, []core.CriterionOutcome{met}, med)        // failed, in the window
	closeTaskAt(t, s, "acme-api", n, core.TaskDone, []core.CriterionOutcome{met}, nil)                   // at N: outside
	closeTaskAt(t, s, "acme-api", day(2026, 12, 1), core.TaskDone, nil, nil)                             // after N: outside
	abandonedAt(t, s, "acme-api", day(2026, 11, 5))                                                      // abandoned: not a close

	// Directives: D1 ratified in June, due in 2027. D2 ratified at October's last instant, due
	// 2026-11-10, so it is expired at N. D3 ratified at N.
	ratifyAt(t, s, day(2026, 6, 1), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "d1", Rule: "r", ReviewBy: "2027-06-01"})
	ratifyAt(t, s, day(2026, 11, 1).Add(-ns), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "d2", Rule: "r", ReviewBy: "2026-11-10"})
	ratifyAt(t, s, n, core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "d3", Rule: "r", ReviewBy: "2027-02-01"})

	// Running: one active task in each of two projects, started at N, so they tie on updated_at.
	clockAt(s, n)
	first := must(s.TaskStart(agent("acme-api", "main"), core.TaskStartRequest{Title: "run-a", Goal: "g"}))(t)
	second := must(s.TaskStart(agent("acme-web", "main"), core.TaskStartRequest{Title: "run-b", Goal: "g"}))(t)
	blocked := must(s.TaskStart(agent("acme-ops", "main"), core.TaskStartRequest{Title: "blk", Goal: "g"}))(t)
	must(s.TaskUpdate(agent("acme-ops", "main"), core.TaskUpdateRequest{ID: blocked.ID, Status: ptrStatus(core.TaskBlocked), Reason: "waiting"}))(t)
	// Two proposed learnings; one is accepted, so one is proposed.
	l1 := mustLearning(agent("acme-api", "main"), t, s, core.LearningAddRequest{Kind: core.LearningFinding, Title: "x", Body: "b"})
	mustLearning(agent("acme-api", "main"), t, s, core.LearningAddRequest{Kind: core.LearningFinding, Title: "y", Body: "b"})
	must(s.LearningDecide(opCtx(), core.LearningDecideRequest{ID: l1.ID, Status: core.LearningAccepted}))(t)

	d := must(s.Dashboard(viewerCtx()))(t)

	if d.Running.Count != 2 || len(d.Running.Tasks) != 2 {
		t.Fatalf("running = %d, %d tasks; want 2 and 2", d.Running.Count, len(d.Running.Tasks))
	}
	if got := taskIDs(d.Running.Tasks); !slices.Equal(got, []string{second.ID, first.ID}) {
		t.Errorf("running tasks = %v, want %v: the tie is broken by id descending", got, []string{second.ID, first.ID})
	}
	if d.Waiting.ProposedLearnings != 1 || d.Waiting.BlockedTasks != 1 || d.Waiting.ExpiredDirectives != 1 {
		t.Errorf("waiting = %+v, want proposed 1, blocked 1, expired 1", d.Waiting)
	}

	// Four done or failed closes in the window: A (30 days before N), October's last instant,
	// November's first instant, and the failed one a nanosecond before N. Of their criteria,
	// A has met and unmet, the failed one met: three results, two met. One high mistake, A's.
	wantQuality := core.DashboardQuality{Closed: 4, Done: 3, CriteriaMet: 2, CriteriaTotal: 3, HighImpactMistakes: 1}
	if d.Quality != wantQuality {
		t.Errorf("quality_30d = %+v, want %+v", d.Quality, wantQuality)
	}

	// Six months, oldest first. June has the June close; October has the close at A, the close at
	// October's last instant, and the abandoned task is in none. November has the first-instant
	// close, the failed close and the close at N; the one at December 1 is in none.
	wantMonths := []core.DashboardMonth{
		{Month: "2026-06", Closed: 1, Done: 1, DirectivesRatified: 1},
		{Month: "2026-07"},
		{Month: "2026-08"},
		{Month: "2026-09"},
		{Month: "2026-10", Closed: 3, Done: 3, HighImpactMistakes: 1, DirectivesRatified: 1},
		{Month: "2026-11", Closed: 3, Done: 2, DirectivesRatified: 1},
	}
	if !slices.Equal(d.Monthly, wantMonths) {
		t.Errorf("monthly = %+v, want %+v", d.Monthly, wantMonths)
	}

	// Three active, one of them expired at N. The oldest review_by is the expired one's day. The
	// oldest age is from June 1 00:00 to N: 167 whole days.
	if d.Directives.Active != 3 || d.Directives.Expired != 1 {
		t.Errorf("directives = %+v, want 3 active and 1 expired", d.Directives)
	}
	if d.Directives.OldestReviewBy != "2026-11-10" {
		t.Errorf("oldest_review_by = %q, want 2026-11-10", d.Directives.OldestReviewBy)
	}
	if d.Directives.OldestAgeDays == nil || *d.Directives.OldestAgeDays != 167 {
		t.Errorf("oldest_age_days = %v, want 167", d.Directives.OldestAgeDays)
	}
}

// TestDashboardOmitsOldestWithNoActiveDirective checks CONTRACT §8.1: oldest_review_by and
// oldest_age_days are left out when no directive is active, and an age of zero is written when
// there is one.
func TestDashboardOmitsOldestWithNoActiveDirective(t *testing.T) {
	s, _ := openLocal(t)
	clockAt(s, t0)
	d := must(s.Dashboard(viewerCtx()))(t)
	raw, err := json.Marshal(d.Directives)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); got != `{"active":0,"expired":0}` {
		t.Errorf("directives with none active = %s, want the two counts only", got)
	}

	must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "now", Rule: "r"}))(t)
	d = must(s.Dashboard(viewerCtx()))(t)
	raw, err = json.Marshal(d.Directives)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(raw); !strings.Contains(got, `"oldest_review_by":"`) || !strings.Contains(got, `"oldest_age_days":0`) {
		t.Errorf("directives with one just ratified = %s, want oldest_review_by and oldest_age_days 0", got)
	}
}

// closeTaskAt starts a task in project as its agent, and closes it at the given time with
// outcome, one criterion per entry in criteria, and mistakes. The clock is set to at for both.
func closeTaskAt(t *testing.T, s *core.Local, project string, at time.Time, outcome core.TaskStatus,
	criteria []core.CriterionOutcome, mistakes []core.Mistake) {
	t.Helper()
	ctx := agent(project, "main")
	clockAt(s, at)
	var brief []string
	for i := range criteria {
		brief = append(brief, fmt.Sprintf("c%d", i+1))
	}
	res := must(s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g", Criteria: brief}))(t)
	req := core.TaskCloseRequest{
		ID: res.ID, Outcome: outcome, Summary: "s", Criteria: criteria,
		Lessons:  []core.Lesson{{Kind: core.LessonReuse, Text: "x", Why: "y"}},
		Mistakes: []core.Mistake{},
	}
	if mistakes != nil {
		req.Mistakes = mistakes
	}
	must(s.TaskClose(ctx, req))(t)
}

// abandonedAt starts a task in acme-api at at, and abandons it there. An abandoned task sets
// closed_at, and no figure counts it.
func abandonedAt(t *testing.T, s *core.Local, project string, at time.Time) {
	t.Helper()
	ctx := agent(project, "main")
	clockAt(s, at)
	res := must(s.TaskStart(ctx, core.TaskStartRequest{Title: "dropped", Goal: "g"}))(t)
	must(s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: res.ID, Status: ptrStatus(core.TaskAbandoned), Reason: "stopped"}))(t)
}

// ratifyAt ratifies a directive with the clock at at.
func ratifyAt(t *testing.T, s *core.Local, at time.Time, req core.DirectiveRatifyRequest) {
	t.Helper()
	clockAt(s, at)
	must(s.DirectiveRatify(opCtx(), req))(t)
}

// ptrStatus returns a pointer to a task status, for an update that sets one.
func ptrStatus(v core.TaskStatus) *core.TaskStatus { return &v }

// projectNames returns the names of ps in order.
func projectNames(ps []core.Project) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

// taskIDs returns the ids of ts in order.
func taskIDs(ts []core.TaskSummary) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}
