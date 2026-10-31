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
	{"identity/viewer_first_read_is_not_found", func(t T, s core.Service) {
		// CONTRACT §1.1 and §9 item 44: a viewer's read of a project that no agent has
		// written to is not_found. The viewer does not create the row; the agent's read
		// after it does, and then the viewer reads the project.
		const project = "viewer-first-read"
		viewer := core.WithCaller(context.Background(), core.Caller{Project: project, Role: core.RoleViewer})
		_, err := s.TaskCurrent(viewer)
		wantKind(t, err, core.KindNotFound, "TaskCurrent by a viewer on a project with no row")
		must(s.TaskCurrent(as(project, "main")))(t)
		must(s.TaskCurrent(viewer))(t)
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
	{"identity/agent_name_control_character_refused", func(t T, s core.Service) {
		// CONTRACT §1.1, step 8 review W3: a name the wire form cannot carry is
		// refused on every agent call, reads included, and the message names the
		// field. The client refuses it before it sends, so it answers the same.
		for _, c := range agentCalls {
			err := c.do(as(projA, "ma\nin"), s)
			wantKind(t, err, core.KindInvalid, c.name+" with a control character in the agent name")
			if err != nil && !strings.Contains(err.Error(), "agent name") {
				t.Errorf("%s: error %q does not name the agent name", c.name, err)
			}
		}
	}},
	{"identity/agent_name_edge_space_refused", func(t T, s core.Service) {
		// A space at either end is not kept by the header form, so it is refused
		// rather than trimmed. A space between words is fine (the next case).
		for _, name := range []string{" main", "main "} {
			for _, c := range agentCalls {
				err := c.do(as(projA, name), s)
				wantKind(t, err, core.KindInvalid, c.name+" with the agent name "+name)
			}
		}
	}},
	{"identity/client_name_refused", func(t T, s core.Service) {
		// The client name comes from the MCP client, and the same rule applies
		// to it: a control character or an edge space is refused, and the
		// message names the client name.
		for _, client := range []string{"claude\tcode", " claude-code"} {
			ctx := core.WithCaller(context.Background(), core.Caller{Project: projA, Agent: "main", Client: client, Role: core.RoleAgent})
			_, err := s.TaskCurrent(ctx)
			wantKind(t, err, core.KindInvalid, "TaskCurrent with the client name "+client)
			if err != nil && !strings.Contains(err.Error(), "client name") {
				t.Errorf("TaskCurrent: error %q does not name the client name", err)
			}
			_, err = s.LearningAdd(ctx, core.LearningAddRequest{Kind: core.LearningFinding, Title: "t", Body: "b"})
			wantKind(t, err, core.KindInvalid, "LearningAdd with the client name "+client)
		}
	}},
	{"identity/session_name_refused", func(t T, s core.Service) {
		// The session is optional, and the header carries it on the wire, so the
		// same rule applies to it (step 10 review S4): a control character or an
		// edge space is refused, on a read and a write, and the message names it.
		for _, session := range []string{"sess\nion", " session"} {
			ctx := core.WithCaller(context.Background(), core.Caller{Project: projA, Agent: "main", Session: session, Client: "conformance", Role: core.RoleAgent})
			_, err := s.TaskCurrent(ctx)
			wantKind(t, err, core.KindInvalid, "TaskCurrent with the session "+session)
			if err != nil && !strings.Contains(err.Error(), "session") {
				t.Errorf("TaskCurrent: error %q does not name the session", err)
			}
			_, err = s.LearningAdd(ctx, core.LearningAddRequest{Kind: core.LearningFinding, Title: "t", Body: "b"})
			wantKind(t, err, core.KindInvalid, "LearningAdd with the session "+session)
		}
	}},
	{"identity/agent_name_inner_space_accepted", func(t T, s core.Service) {
		// Only the edges and the control characters are refused: a name may
		// hold a space between its words, and it is kept as given.
		l := must(s.LearningAdd(as(projA, "code reviewer"), core.LearningAddRequest{Kind: core.LearningFinding, Title: "t", Body: "b"}))(t)
		if l.Agent != "code reviewer" {
			t.Errorf("agent = %q, want %q", l.Agent, "code reviewer")
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
