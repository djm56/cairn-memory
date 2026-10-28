package conformance

import (
	"regexp"
	"slices"

	"github.com/djm56/cairn-memory/internal/core"
)

var directiveCode = regexp.MustCompile(`^D-\d{4,}$`)

// directiveTitles returns the titles of ds in order.
func directiveTitles(ds []core.Directive) []string {
	var titles []string
	for _, d := range ds {
		titles = append(titles, d.Title)
	}
	return titles
}

var learningCases = []Case{
	{"learning/add_is_proposed_only", func(t T, s core.Service) {
		for _, k := range []core.LearningKind{core.LearningFinding, core.LearningPattern, core.LearningAgentNote} {
			l := must(s.LearningAdd(mainCtx(), core.LearningAddRequest{Kind: k, Title: string(k), Body: "b"}))(t)
			if l.Status != core.LearningProposed || l.Agent != "main" || l.Project != projA {
				t.Errorf("new %s = %+v", k, l)
			}
		}
		_, err := s.LearningAdd(mainCtx(), core.LearningAddRequest{Kind: "rumour", Title: "t", Body: "b"})
		wantKind(t, err, core.KindInvalid, "unknown learning kind")
		_, err = s.LearningAdd(mainCtx(), core.LearningAddRequest{Kind: core.LearningFinding, Title: "t"})
		wantKind(t, err, core.KindInvalid, "learning without body")
		_, err = s.LearningAdd(mainCtx(), core.LearningAddRequest{Kind: core.LearningFinding, Title: "t", Body: "b", TaskID: "task-19990101-01"})
		wantKind(t, err, core.KindNotFound, "learning for an unknown task")
	}},
	{"learning/operator_decides", func(t T, s core.Service) {
		a := must(s.LearningAdd(mainCtx(), core.LearningAddRequest{Kind: core.LearningFinding, Title: "a", Body: "b", Target: "reviewer"}))(t)
		b := must(s.LearningAdd(mainCtx(), core.LearningAddRequest{Kind: core.LearningFinding, Title: "b", Body: "b"}))(t)
		c := must(s.LearningAdd(as(projB, "main"), core.LearningAddRequest{Kind: core.LearningPattern, Title: "c", Body: "b"}))(t)

		got := must(s.LearningDecide(op(), core.LearningDecideRequest{ID: a.ID, Status: core.LearningAccepted, Note: "good catch"}))(t)
		if got.Status != core.LearningAccepted || got.DecidedBy != operator || got.DecidedAt == nil || got.DecisionNote != "good catch" {
			t.Errorf("accepted learning = %+v", got)
		}
		_, err := s.LearningDecide(op(), core.LearningDecideRequest{ID: b.ID, Status: core.LearningSuperseded})
		wantKind(t, err, core.KindInvalid, "superseded without superseded_by")
		got = must(s.LearningDecide(op(), core.LearningDecideRequest{ID: b.ID, Status: core.LearningSuperseded, SupersededBy: &a.ID}))(t)
		if got.SupersededBy == nil || *got.SupersededBy != a.ID {
			t.Errorf("superseded learning = %+v", got)
		}
		_, err = s.LearningDecide(op(), core.LearningDecideRequest{ID: c.ID, Status: core.LearningProposed})
		wantKind(t, err, core.KindInvalid, "deciding back to proposed")
		_, err = s.LearningDecide(op(), core.LearningDecideRequest{ID: 999999, Status: core.LearningRejected})
		wantKind(t, err, core.KindNotFound, "unknown learning")

		proposed := must(s.LearningList(mainCtx(), core.LearningListRequest{Status: []core.LearningStatus{core.LearningProposed}}))(t)
		if len(proposed.Learnings) != 0 {
			t.Errorf("proposed in %s = %+v, want none (c is in %s)", projA, proposed.Learnings, projB)
		}
		all := must(s.LearningList(mainCtx(), core.LearningListRequest{AllProjects: true}))(t)
		if len(all.Learnings) != 3 {
			t.Errorf("all projects = %d learnings, want 3", len(all.Learnings))
		}
		byTarget := must(s.LearningList(mainCtx(), core.LearningListRequest{Target: "reviewer"}))(t)
		if len(byTarget.Learnings) != 1 || byTarget.Learnings[0].ID != a.ID {
			t.Errorf("by target = %+v", byTarget.Learnings)
		}
		patterns := must(s.LearningList(mainCtx(), core.LearningListRequest{AllProjects: true, Kind: []core.LearningKind{core.LearningPattern}}))(t)
		if len(patterns.Learnings) != 1 || patterns.Learnings[0].ID != c.ID {
			t.Errorf("patterns = %+v", patterns.Learnings)
		}
	}},
	{"directive/scopes_apply", func(t T, s core.Service) {
		ratify := func(req core.DirectiveRatifyRequest) *core.Directive {
			t.Helper()
			d := must(s.DirectiveRatify(op(), req))(t)
			if !directiveCode.MatchString(d.Code) || d.Status != core.DirectiveActive || d.RatifiedBy != operator {
				t.Errorf("ratified directive = %+v", d)
			}
			return d
		}
		ratify(core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "global", Rule: "r"})
		ratify(core.DirectiveRatifyRequest{Scope: core.ScopeProject, Project: projA, Title: "project-a", Rule: "r"})
		ratify(core.DirectiveRatifyRequest{Scope: core.ScopeProject, Project: projB, Title: "project-b", Rule: "r"})
		ratify(core.DirectiveRatifyRequest{Scope: core.ScopeAgent, Agent: "reviewer", Title: "reviewer-only", Rule: "r"})
		retired := ratify(core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "retired", Rule: "r"})
		got := must(s.DirectiveRetire(op(), core.DirectiveRetireRequest{Code: retired.Code}))(t)
		if got.Status != core.DirectiveRetired || got.RetiredAt == nil {
			t.Errorf("retired directive = %+v", got)
		}

		_, err := s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeProject, Title: "t", Rule: "r"})
		wantKind(t, err, core.KindInvalid, "project scope without a project")
		_, err = s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeAgent, Title: "t", Rule: "r"})
		wantKind(t, err, core.KindInvalid, "agent scope without an agent")
		_, err = s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t"})
		wantKind(t, err, core.KindInvalid, "directive without a rule")

		want := func(req core.DirectiveListRequest, titles ...string) {
			t.Helper()
			ds := must(s.DirectiveList(mainCtx(), req))(t)
			got := directiveTitles(ds.Directives)
			slices.Sort(got)
			slices.Sort(titles)
			if !slices.Equal(got, titles) {
				t.Errorf("directive_list(%+v) in %s = %v, want %v", req, projA, got, titles)
			}
		}
		want(core.DirectiveListRequest{}, "global", "project-a", "reviewer-only")
		want(core.DirectiveListRequest{Agent: "reviewer"}, "global", "project-a", "reviewer-only")
		want(core.DirectiveListRequest{Agent: "main"}, "global", "project-a")

		startTask(t, s, mainCtx(), "t")
		cur := must(s.TaskCurrent(mainCtx()))(t)
		if got := directiveTitles(cur.Directives); !slices.Contains(got, "global") || slices.Contains(got, "project-b") || slices.Contains(got, "retired") {
			t.Errorf("TaskCurrent directives = %v", got)
		}
	}},
	{"directive/from_learning", func(t T, s core.Service) {
		l := must(s.LearningAdd(mainCtx(), core.LearningAddRequest{Kind: core.LearningFinding, Title: "Check redirects", Body: "b"}))(t)
		req := core.DirectiveRatifyRequest{FromLearning: &l.ID, Scope: core.ScopeGlobal, Title: "Check redirects", Rule: "Every redirect has an origin check."}
		_, err := s.DirectiveRatify(op(), req)
		wantKind(t, err, core.KindRefused, "ratifying a proposed learning")
		must(s.LearningDecide(op(), core.LearningDecideRequest{ID: l.ID, Status: core.LearningAccepted}))(t)
		d := must(s.DirectiveRatify(op(), req))(t)
		if d.SourceLearning == nil || *d.SourceLearning != l.ID {
			t.Errorf("directive source = %v, want %d", d.SourceLearning, l.ID)
		}
		after := must(s.LearningList(mainCtx(), core.LearningListRequest{}))(t)
		if len(after.Learnings) != 1 || after.Learnings[0].Status != core.LearningApplied {
			t.Errorf("learning after ratification = %+v, want applied", after.Learnings)
		}
		// The rule reaches another project's next session.
		ds := must(s.DirectiveList(as(projB, "main"), core.DirectiveListRequest{}))(t)
		if len(ds.Directives) != 1 || ds.Directives[0].Code != d.Code {
			t.Errorf("directives in %s = %+v", projB, ds.Directives)
		}
	}},
}

var searchCases = []Case{
	{"recall/across_projects", func(t T, s core.Service) {
		ctxB := as(projB, "main")
		res := must(s.TaskStart(ctxB, core.TaskStartRequest{Title: "Migrate webhook signatures", Goal: "Verify webhook signatures with HMAC-SHA256."}))(t)
		req := fullClose(res.ID, 0)
		req.Lessons = []core.Lesson{{Kind: core.LessonWrongAssumption, Text: "Webhook bodies must be verified before JSON decoding.", Why: "Decoding reorders keys."}}
		req.Mistakes = []core.Mistake{{Agent: "main", Mistake: "Compared webhook signatures with ==", Impact: core.ImpactHigh, Prevention: "Use hmac.Equal"}}
		must(s.TaskClose(ctxB, req))(t)
		must(s.TaskStart(ctxB, core.TaskStartRequest{Title: "Unrelated chore", Goal: "Bump the linter.", Queue: true}))(t)
		must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "Constant-time compare", Rule: "Compare secrets with constant-time functions."}))(t)

		got := must(s.Recall(mainCtx(), core.RecallRequest{Goal: "Add webhook signature checks to the API"}))(t)
		if len(got.Lessons) == 0 || got.Lessons[0].TaskID != res.ID || got.Lessons[0].Project != projB {
			t.Errorf("recall lessons = %+v, want the %s lesson from %s", got.Lessons, projB, res.ID)
		}
		if len(got.Mistakes) == 0 || got.Mistakes[0].TaskID != res.ID {
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
		if titles := directiveTitles(got.Directives); !slices.Contains(titles, "Constant-time compare") {
			t.Errorf("recall directives = %v", titles)
		}
		_, err := s.Recall(mainCtx(), core.RecallRequest{})
		wantKind(t, err, core.KindInvalid, "recall without a goal")
	}},
	{"search/finds_each_kind", func(t T, s core.Service) {
		res := must(s.TaskStart(mainCtx(), core.TaskStartRequest{Title: "Tune the zeppelin cache", Goal: "Cache hit rate over 90%."}))(t)
		must(s.EventLog(mainCtx(), core.EventLogRequest{TaskID: res.ID, Kind: core.EventNote, Text: "The quokka endpoint is slow."}))(t)
		must(s.LearningAdd(mainCtx(), core.LearningAddRequest{Kind: core.LearningPattern, Title: "Narwhal batching", Body: "Batch writes."}))(t)
		must(s.NotePut(mainCtx(), core.NotePutRequest{TaskID: res.ID, Title: "Design", Body: "The axolotl layer sits in front."}))(t)
		must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "Pangolin rule", Rule: "Never skip the pangolin check."}))(t)

		for word, kind := range map[string]core.HitKind{
			"zeppelin": core.HitTask, "quokka": core.HitEvent, "narwhal": core.HitLearning,
			"axolotl": core.HitNote, "pangolin": core.HitDirective,
		} {
			got := must(s.Search(mainCtx(), core.SearchRequest{Query: word}))(t)
			if len(got.Hits) == 0 || got.Hits[0].Kind != kind {
				t.Errorf("search %q = %+v, want a %s first", word, got.Hits, kind)
				continue
			}
			if got.Hits[0].Snippet == "" {
				t.Errorf("search %q hit has no snippet", word)
			}
		}
		only := must(s.Search(mainCtx(), core.SearchRequest{Query: "zeppelin", Kinds: []core.HitKind{core.HitEvent}}))(t)
		if len(only.Hits) != 0 {
			t.Errorf("search limited to events found %+v", only.Hits)
		}
		// Another project's rows need all_projects.
		mine := must(s.Search(as(projB, "main"), core.SearchRequest{Query: "quokka"}))(t)
		if len(mine.Hits) != 0 {
			t.Errorf("search in %s found %s rows: %+v", projB, projA, mine.Hits)
		}
		all := must(s.Search(as(projB, "main"), core.SearchRequest{Query: "quokka", AllProjects: true}))(t)
		if len(all.Hits) == 0 {
			t.Errorf("all_projects search found nothing")
		}
		// A query is text, never syntax: punctuation must not error.
		must(s.Search(mainCtx(), core.SearchRequest{Query: `"unbalanced AND (OR* -`}))(t)
		_, err := s.Search(mainCtx(), core.SearchRequest{})
		wantKind(t, err, core.KindInvalid, "empty query")
	}},
	{"note/put_and_get", func(t T, s core.Service) {
		res := startTask(t, s, mainCtx(), "t")
		n := must(s.NotePut(mainCtx(), core.NotePutRequest{TaskID: res.ID, Title: "Design", Body: "# Design\n\nv1"}))(t)
		if n.ID == 0 || n.TaskID != res.ID || n.CreatedBy != "main" {
			t.Errorf("new note = %+v", n)
		}
		n2 := must(s.NotePut(mainCtx(), core.NotePutRequest{ID: n.ID, Title: "Design", Body: "# Design\n\nv2"}))(t)
		if n2.ID != n.ID {
			t.Errorf("update created note %d, want %d", n2.ID, n.ID)
		}
		got := must(s.NoteGet(mainCtx(), core.NoteGetRequest{ID: n.ID}))(t)
		if got.Body != "# Design\n\nv2" {
			t.Errorf("note body = %q", got.Body)
		}
		_, err := s.NoteGet(as(projB, "main"), core.NoteGetRequest{ID: n.ID})
		wantKind(t, err, core.KindNotFound, "note from another project")
		_, err = s.NotePut(mainCtx(), core.NotePutRequest{Title: "t", Body: "b", TaskID: "task-19990101-01"})
		wantKind(t, err, core.KindNotFound, "note on an unknown task")
	}},
}
