package conformance

import (
	"context"
	"reflect"
	"sort"
	"strings"

	"github.com/djm56/cairn-memory/internal/core"
)

// call is one Service method invoked with plausible arguments, so the only
// thing wrong with it is the caller.
type call struct {
	name  string
	write bool
	do    func(ctx context.Context, s core.Service) error
}

func ignore[V any](_ V, err error) error { return err }

// agentCalls has one entry per method of core.Agent; casesCoverInterface
// checks that none is missing.
var agentCalls = []call{
	{"TaskCurrent", false, func(ctx context.Context, s core.Service) error { return ignore(s.TaskCurrent(ctx)) }},
	{"TaskStart", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g"}))
	}},
	{"TaskGet", false, func(ctx context.Context, s core.Service) error {
		return ignore(s.TaskGet(ctx, core.TaskGetRequest{ID: "task-20260101-01"}))
	}},
	{"TaskList", false, func(ctx context.Context, s core.Service) error {
		return ignore(s.TaskList(ctx, core.TaskListRequest{}))
	}},
	{"TaskUpdate", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.TaskUpdate(ctx, core.TaskUpdateRequest{ID: "task-20260101-01", NextAction: ptr("x")}))
	}},
	{"TaskClose", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.TaskClose(ctx, fullClose("task-20260101-01", 0)))
	}},
	{"StepPlan", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.StepPlan(ctx, core.StepPlanRequest{TaskID: "task-20260101-01", Steps: []core.StepInput{{Title: "a"}}}))
	}},
	{"StepAdd", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.StepAdd(ctx, core.StepAddRequest{TaskID: "task-20260101-01", Title: "a"}))
	}},
	{"StepUpdate", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.StepUpdate(ctx, core.StepUpdateRequest{TaskID: "task-20260101-01", Step: core.NumberRef(1), Action: core.StepStart}))
	}},
	{"EventLog", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.EventLog(ctx, core.EventLogRequest{TaskID: "task-20260101-01", Kind: core.EventNote, Text: "x"}))
	}},
	{"LearningAdd", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.LearningAdd(ctx, core.LearningAddRequest{Kind: core.LearningFinding, Title: "t", Body: "b"}))
	}},
	{"LearningList", false, func(ctx context.Context, s core.Service) error {
		return ignore(s.LearningList(ctx, core.LearningListRequest{}))
	}},
	{"DirectiveList", false, func(ctx context.Context, s core.Service) error {
		return ignore(s.DirectiveList(ctx, core.DirectiveListRequest{}))
	}},
	{"Recall", false, func(ctx context.Context, s core.Service) error {
		return ignore(s.Recall(ctx, core.RecallRequest{Goal: "g"}))
	}},
	{"Search", false, func(ctx context.Context, s core.Service) error {
		return ignore(s.Search(ctx, core.SearchRequest{Query: "q"}))
	}},
}

// operatorCalls has one entry per method of core.Operator.
var operatorCalls = []call{
	{"LearningDecide", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.LearningDecide(ctx, core.LearningDecideRequest{ID: 1, Status: core.LearningAccepted}))
	}},
	{"DirectiveRatify", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.DirectiveRatify(ctx, core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t", Rule: "r"}))
	}},
	{"DirectiveRetire", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.DirectiveRetire(ctx, core.DirectiveRetireRequest{Code: "D-0001"}))
	}},
	{"ProjectSettingsSet", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.ProjectSettingsSet(ctx, core.ProjectSettingsRequest{Project: projA, Settings: core.DefaultSettings().AsInput()}))
	}},
}

var identityCases = []Case{
	{"identity/operator_blank_name_refused", func(t T, s core.Service) {
		// CONTRACT §1.1, §9 item 34: a name of only spaces is no name, on an
		// operator call as on an agent write.
		l := must(s.LearningAdd(mainCtx(), core.LearningAddRequest{Kind: core.LearningFinding, Title: "t", Body: "b"}))(t)
		spaces := core.WithCaller(context.Background(), core.Caller{Role: core.RoleOperator, Agent: "   "})
		_, err := s.LearningDecide(spaces, core.LearningDecideRequest{ID: l.ID, Status: core.LearningAccepted})
		wantKind(t, err, core.KindInvalid, "LearningDecide with a name of spaces")
		_, err = s.ProjectSettingsSet(spaces, core.ProjectSettingsRequest{Project: projA, Settings: core.DefaultSettings().AsInput()})
		wantKind(t, err, core.KindInvalid, "ProjectSettingsSet with a name of spaces")
		got := must(s.LearningList(mainCtx(), core.LearningListRequest{}))(t)
		if len(got.Learnings) != 1 || got.Learnings[0].Status != core.LearningProposed {
			t.Errorf("learnings after refused calls = %+v, want the one still proposed", got.Learnings)
		}
	}},
	{"identity/calls_cover_interface", casesCoverInterface},
	{"identity/no_project_refused", func(t T, s core.Service) {
		noProject := core.WithCaller(context.Background(), core.Caller{Agent: "main", Role: core.RoleAgent})
		for _, c := range agentCalls {
			err := c.do(noProject, s)
			wantKind(t, err, core.KindInvalid, c.name+" without a project")
			if err != nil && !strings.Contains(err.Error(), "cairn init") {
				t.Errorf("%s without a project: error %q does not name `cairn init`", c.name, err)
			}
			wantKind(t, c.do(context.Background(), s), core.KindInvalid, c.name+" without any caller")
		}
	}},
	{"identity/bad_project_name_refused", func(t T, s core.Service) {
		for _, name := range []string{"Acme", "acme_api", "-acme", "acme-", "acme--api", "acme api", strings.Repeat("a", 65)} {
			_, err := s.TaskCurrent(as(name, "main"))
			wantKind(t, err, core.KindInvalid, "project name "+name)
		}
	}},
	{"identity/write_without_agent_refused", func(t T, s core.Service) {
		noAgent := as(projA, "")
		for _, c := range agentCalls {
			if c.write {
				wantKind(t, c.do(noAgent, s), core.KindInvalid, c.name+" without an agent")
			}
		}
	}},
	{"identity/operator_calls_refuse_agents", func(t T, s core.Service) {
		// A real learning and directive exist, so a refusal can only come
		// from the caller's role.
		l := must(s.LearningAdd(mainCtx(), core.LearningAddRequest{Kind: core.LearningFinding, Title: "t", Body: "b"}))(t)
		must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t", Rule: "r"}))(t)
		for _, c := range operatorCalls {
			wantKind(t, c.do(mainCtx(), s), core.KindOperatorOnly, c.name+" by an agent")
			wantKind(t, c.do(context.Background(), s), core.KindOperatorOnly, c.name+" without any caller")
		}
		after := must(s.LearningList(mainCtx(), core.LearningListRequest{}))(t)
		if len(after.Learnings) != 1 || after.Learnings[0].ID != l.ID || after.Learnings[0].Status != core.LearningProposed {
			t.Errorf("learning changed by refused calls: %+v", after.Learnings)
		}
		ds := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
		if len(ds.Directives) != 1 || ds.Directives[0].Status != core.DirectiveActive {
			t.Errorf("directives changed by refused calls: %+v", ds.Directives)
		}
	}},
	{"identity/operator_call_needs_name", func(t T, s core.Service) {
		// CONTRACT §8: every operator call needs the operator's name. Each
		// call below is valid but for the name, and the learning and the
		// directive exist, so a call that got past the name would change them.
		l := must(s.LearningAdd(mainCtx(), core.LearningAddRequest{Kind: core.LearningFinding, Title: "t", Body: "b"}))(t)
		d := must(s.DirectiveRatify(op(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t", Rule: "r"}))(t)
		nameless := core.WithCaller(context.Background(), core.Caller{Role: core.RoleOperator})
		_, err := s.LearningDecide(nameless, core.LearningDecideRequest{ID: l.ID, Status: core.LearningAccepted})
		wantKind(t, err, core.KindInvalid, "LearningDecide without a name")
		_, err = s.DirectiveRatify(nameless, core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t2", Rule: "r"})
		wantKind(t, err, core.KindInvalid, "DirectiveRatify without a name")
		_, err = s.DirectiveRetire(nameless, core.DirectiveRetireRequest{Code: d.Code})
		wantKind(t, err, core.KindInvalid, "DirectiveRetire without a name")
		_, err = s.ProjectSettingsSet(nameless, core.ProjectSettingsRequest{Project: projA, Settings: core.DefaultSettings().AsInput()})
		wantKind(t, err, core.KindInvalid, "ProjectSettingsSet without a name")

		after := must(s.LearningList(mainCtx(), core.LearningListRequest{}))(t)
		if len(after.Learnings) != 1 || after.Learnings[0].Status != core.LearningProposed {
			t.Errorf("learning changed by nameless calls: %+v", after.Learnings)
		}
		ds := must(s.DirectiveList(mainCtx(), core.DirectiveListRequest{}))(t)
		if len(ds.Directives) != 1 || ds.Directives[0].Status != core.DirectiveActive {
			t.Errorf("directives changed by nameless calls: %+v", ds.Directives)
		}
	}},
	{"identity/operator_role_before_shape", func(t T, s core.Service) {
		// CONTRACT §8: each operator call refuses a caller without the operator
		// role before any other check. Each request below is invalid as well,
		// so an agent that gets invalid instead has had the shape checked first.
		calls := []struct {
			name string
			do   func(ctx context.Context) error
		}{
			{"LearningDecide back to proposed", func(ctx context.Context) error {
				return ignore(s.LearningDecide(ctx, core.LearningDecideRequest{ID: 1, Status: core.LearningProposed}))
			}},
			{"LearningDecide with an unknown status", func(ctx context.Context) error {
				return ignore(s.LearningDecide(ctx, core.LearningDecideRequest{ID: 1, Status: "gossip"}))
			}},
			{"DirectiveRatify with an unknown scope and no title", func(ctx context.Context) error {
				return ignore(s.DirectiveRatify(ctx, core.DirectiveRatifyRequest{Scope: "galaxy"}))
			}},
			{"DirectiveRetire with a blank code", func(ctx context.Context) error {
				return ignore(s.DirectiveRetire(ctx, core.DirectiveRetireRequest{Code: " "}))
			}},
			{"ProjectSettingsSet with a bad name and no settings", func(ctx context.Context) error {
				return ignore(s.ProjectSettingsSet(ctx, core.ProjectSettingsRequest{Project: "Acme"}))
			}},
		}
		for _, c := range calls {
			wantKind(t, c.do(mainCtx()), core.KindOperatorOnly, c.name+" by an agent")
		}
	}},
	{"identity/blank_agent_refused", func(t T, s core.Service) {
		// CONTRACT §1.1: a write needs an agent name. A name made of spaces is no
		// name, and it is refused for every write the same way.
		spaces := as(projA, "   ")
		for _, c := range agentCalls {
			if c.write {
				wantKind(t, c.do(spaces, s), core.KindInvalid, c.name+" with a blank agent")
			}
		}
	}},
}

// casesCoverInterface fails when a Service method has no entry in
// agentCalls or operatorCalls, so a new method cannot skip the identity
// checks. It touches the Service only to stay a normal case.
func casesCoverInterface(t T, s core.Service) {
	check := func(iface reflect.Type, calls []call) {
		have := map[string]bool{}
		for _, c := range calls {
			have[c.name] = true
		}
		var missing []string
		for i := 0; i < iface.NumMethod(); i++ {
			if name := iface.Method(i).Name; !have[name] {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s methods with no identity check: %v", iface.Name(), missing)
		}
		if len(calls) != iface.NumMethod() {
			t.Errorf("%s: %d calls listed for %d methods", iface.Name(), len(calls), iface.NumMethod())
		}
	}
	check(reflect.TypeOf((*core.Agent)(nil)).Elem(), agentCalls)
	check(reflect.TypeOf((*core.Operator)(nil)).Elem(), operatorCalls)
	must(s.TaskCurrent(mainCtx()))(t)
}
