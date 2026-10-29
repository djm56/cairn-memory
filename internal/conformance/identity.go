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
	{"EventList", false, func(ctx context.Context, s core.Service) error {
		return ignore(s.EventList(ctx, core.EventListRequest{TaskID: "task-20260101-01"}))
	}},
	{"ReviewRecord", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.ReviewRecord(ctx, core.ReviewRecordRequest{TaskID: "task-20260101-01", Step: core.NumberRef(1), Verdict: core.VerdictApproved}))
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
	{"NotePut", true, func(ctx context.Context, s core.Service) error {
		return ignore(s.NotePut(ctx, core.NotePutRequest{Title: "t", Body: "b"}))
	}},
	{"NoteGet", false, func(ctx context.Context, s core.Service) error {
		return ignore(s.NoteGet(ctx, core.NoteGetRequest{ID: 1}))
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
		return ignore(s.ProjectSettingsSet(ctx, core.ProjectSettingsRequest{Project: projA, Settings: core.DefaultSettings()}))
	}},
}

var identityCases = []Case{
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
