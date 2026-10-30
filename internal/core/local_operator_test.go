package core_test

import (
	"context"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// opCtx is the operator's context. An operator call needs no project, and its
// name is the one the decision records.
func opCtx() context.Context {
	return core.WithCaller(context.Background(), core.Caller{Agent: "operator", Role: core.RoleOperator})
}

// TestOperatorCallNeedsName checks that an operator call with no name is
// invalid, for all four calls. Two of them record the name on the row they
// change (decided_by, ratified_by). The other two store none, and are refused
// too, so the rule is the same for every operator call (CONTRACT §8). Each call
// here is otherwise valid, and the name check runs before any row is read, so a
// missing check would let the call through to the store.
func TestOperatorCallNeedsName(t *testing.T) {
	s, _ := openLocal(t)
	l := mustLearning(mainCtx(), t, s, core.LearningAddRequest{Kind: core.LearningFinding, Title: "t", Body: "b"})
	nameless := core.WithCaller(context.Background(), core.Caller{Role: core.RoleOperator})

	_, err := s.LearningDecide(nameless, core.LearningDecideRequest{ID: l.ID, Status: core.LearningAccepted})
	wantKindLocal(t, err, core.KindInvalid, "LearningDecide without a name")
	_, err = s.DirectiveRatify(nameless, core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t", Rule: "r"})
	wantKindLocal(t, err, core.KindInvalid, "DirectiveRatify without a name")
	_, err = s.DirectiveRetire(nameless, core.DirectiveRetireRequest{Code: "D-0001"})
	wantKindLocal(t, err, core.KindInvalid, "DirectiveRetire without a name")
	_, err = s.ProjectSettingsSet(nameless, core.ProjectSettingsRequest{Project: projA, Settings: core.DefaultSettings().AsInput()})
	wantKindLocal(t, err, core.KindInvalid, "ProjectSettingsSet without a name")

	// Nothing was written: the learning is still proposed.
	got := must(s.LearningList(mainCtx(), core.LearningListRequest{}))(t)
	if len(got.Learnings) != 1 || got.Learnings[0].Status != core.LearningProposed {
		t.Errorf("learnings after nameless decisions = %+v, want one proposed", got.Learnings)
	}
}

// TestLearningDecideShape checks the request-shape checks of learning_decide.
// The learning exists, so a row that gets past a missing check reaches the
// store and is not refused as not_found.
func TestLearningDecideShape(t *testing.T) {
	s, _ := openLocal(t)
	l := mustLearning(mainCtx(), t, s, core.LearningAddRequest{Kind: core.LearningFinding, Title: "t", Body: "b"})
	other := mustLearning(mainCtx(), t, s, core.LearningAddRequest{Kind: core.LearningFinding, Title: "u", Body: "b"})
	self := l.ID
	elsewhere := other.ID

	cases := []struct {
		name string
		req  core.LearningDecideRequest
	}{
		{"back to proposed", core.LearningDecideRequest{ID: l.ID, Status: core.LearningProposed}},
		{"unknown status", core.LearningDecideRequest{ID: l.ID, Status: "gossip"}},
		{"superseded_by on accepted", core.LearningDecideRequest{ID: l.ID, Status: core.LearningAccepted, SupersededBy: &elsewhere}},
		{"superseded by itself", core.LearningDecideRequest{ID: l.ID, Status: core.LearningSuperseded, SupersededBy: &self}},
	}
	for _, c := range cases {
		_, err := s.LearningDecide(opCtx(), c.req)
		wantKindLocal(t, err, core.KindInvalid, c.name)
	}
}

// TestLearningDecideSupersededByMustExist checks that superseded_by names a
// learning that exists. The database would refuse the row with a foreign-key
// error, which is not a kind a client can act on.
func TestLearningDecideSupersededByMustExist(t *testing.T) {
	s, _ := openLocal(t)
	l := mustLearning(mainCtx(), t, s, core.LearningAddRequest{Kind: core.LearningFinding, Title: "t", Body: "b"})
	missing := int64(999999)
	_, err := s.LearningDecide(opCtx(), core.LearningDecideRequest{ID: l.ID, Status: core.LearningSuperseded, SupersededBy: &missing})
	wantKindLocal(t, err, core.KindNotFound, "superseded_by an unknown learning")
}

// TestDirectiveRatifyShape checks the scope and field checks of
// directive_ratify. Each row breaks one rule, and the rest of the request is
// valid, so the row's fault is the one that refuses it.
func TestDirectiveRatifyShape(t *testing.T) {
	s, _ := openLocal(t)
	cases := []struct {
		name string
		req  core.DirectiveRatifyRequest
	}{
		{"unknown scope", core.DirectiveRatifyRequest{Scope: "galaxy", Title: "t", Rule: "r"}},
		{"global with a project", core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Project: projA, Title: "t", Rule: "r"}},
		{"global with an agent", core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Agent: "main", Title: "t", Rule: "r"}},
		{"project scope with a bad name", core.DirectiveRatifyRequest{Scope: core.ScopeProject, Project: "Acme", Title: "t", Rule: "r"}},
		{"project scope with an agent", core.DirectiveRatifyRequest{Scope: core.ScopeProject, Project: projA, Agent: "main", Title: "t", Rule: "r"}},
		{"agent scope with a project", core.DirectiveRatifyRequest{Scope: core.ScopeAgent, Agent: "reviewer", Project: projA, Title: "t", Rule: "r"}},
		{"blank title", core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: " ", Rule: "r"}},
	}
	for _, c := range cases {
		_, err := s.DirectiveRatify(opCtx(), c.req)
		wantKindLocal(t, err, core.KindInvalid, c.name)
	}
	// No row was written, so the first code is still free.
	d := must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t", Rule: "r"}))(t)
	if d.Code != "D-0001" {
		t.Errorf("first code after refused ratifications = %s, want D-0001", d.Code)
	}
}

// TestDirectiveRatifyGlobalScope checks the scope combinations of a global
// directive. It applies to every project and agent, so it is refused with a
// project, with an agent, or with both. Given neither, it is ratified with the
// scope and no project or agent.
func TestDirectiveRatifyGlobalScope(t *testing.T) {
	s, _ := openLocal(t)
	refused := []struct {
		name string
		req  core.DirectiveRatifyRequest
	}{
		{"global with a project", core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Project: projA, Title: "t", Rule: "r"}},
		{"global with an agent", core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Agent: "main", Title: "t", Rule: "r"}},
		{"global with a project and an agent", core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Project: projA, Agent: "main", Title: "t", Rule: "r"}},
	}
	for _, c := range refused {
		_, err := s.DirectiveRatify(opCtx(), c.req)
		wantKindLocal(t, err, core.KindInvalid, c.name)
	}

	d, err := s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t", Rule: "r"})
	if err != nil {
		t.Fatalf("global with neither a project nor an agent: %v", err)
	}
	if d.Scope != core.ScopeGlobal || d.Project != "" || d.Agent != "" {
		t.Errorf("global directive = scope %q project %q agent %q, want global with neither", d.Scope, d.Project, d.Agent)
	}
}

// TestDirectiveRatifyFromUnknownLearning checks that from_learning names a
// learning that exists. The accepted check is covered by the conformance case
// from_learning, which refuses a proposed one.
func TestDirectiveRatifyFromUnknownLearning(t *testing.T) {
	s, _ := openLocal(t)
	missing := int64(999999)
	_, err := s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{FromLearning: &missing, Scope: core.ScopeGlobal, Title: "t", Rule: "r"})
	wantKindLocal(t, err, core.KindNotFound, "from_learning an unknown learning")
}

// TestDirectiveRetireRules checks the rules of directive_retire: a code is
// required, the directive must exist, and a retirement happens once. A second
// retirement is refused and keeps the first time.
func TestDirectiveRetireRules(t *testing.T) {
	s, st := openLocal(t)
	_, err := s.DirectiveRetire(opCtx(), core.DirectiveRetireRequest{Code: " "})
	wantKindLocal(t, err, core.KindInvalid, "retire with a blank code")
	_, err = s.DirectiveRetire(opCtx(), core.DirectiveRetireRequest{Code: "D-9999"})
	wantKindLocal(t, err, core.KindNotFound, "retire an unknown code")

	d := must(s.DirectiveRatify(opCtx(), core.DirectiveRatifyRequest{Scope: core.ScopeGlobal, Title: "t", Rule: "r"}))(t)
	first := must(s.DirectiveRetire(opCtx(), core.DirectiveRetireRequest{Code: d.Code}))(t)
	if first.RetiredAt == nil {
		t.Fatalf("retired directive has no retired_at")
	}
	_, err = s.DirectiveRetire(opCtx(), core.DirectiveRetireRequest{Code: d.Code})
	wantKindLocal(t, err, core.KindRefused, "retire a retired directive")

	row, err := store.GetDirectiveByCode(context.Background(), st, d.Code)
	if err != nil {
		t.Fatal(err)
	}
	if row.RetiredAt == nil || !row.RetiredAt.Equal(*first.RetiredAt) {
		t.Errorf("retired_at after the refused second retire = %v, want the first time %v", row.RetiredAt, first.RetiredAt)
	}
}

// TestProjectSettingsSetRules checks the project name check of
// project_settings_set, and that a nil display name keeps the stored one.
func TestProjectSettingsSetRules(t *testing.T) {
	s, st := openLocal(t)
	for _, name := range []string{"", "Acme"} {
		_, err := s.ProjectSettingsSet(opCtx(), core.ProjectSettingsRequest{Project: name, Settings: core.DefaultSettings().AsInput()})
		wantKindLocal(t, err, core.KindInvalid, "project name "+name)
	}

	// Settings are replaced whole, so an omitted object is refused.
	_, err := s.ProjectSettingsSet(opCtx(), core.ProjectSettingsRequest{Project: projA})
	wantKindLocal(t, err, core.KindInvalid, "project_settings_set with no settings")

	named := "Acme API"
	must(s.ProjectSettingsSet(opCtx(), core.ProjectSettingsRequest{Project: projA, DisplayName: &named, Settings: core.DefaultSettings().AsInput()}))(t)
	settings := core.DefaultSettings()
	settings.ParallelTasks = true
	must(s.ProjectSettingsSet(opCtx(), core.ProjectSettingsRequest{Project: projA, Settings: settings.AsInput()}))(t)

	row, err := store.GetProject(context.Background(), st, projA)
	if err != nil {
		t.Fatal(err)
	}
	if row.DisplayName != named {
		t.Errorf("display name after a call with none = %q, want %q", row.DisplayName, named)
	}
	if !strings.Contains(row.Settings, `"parallel_tasks":true`) {
		t.Errorf("settings = %s, want parallel_tasks true", row.Settings)
	}
}

// TestProjectSettingsSetNeedsEverySwitch checks that a settings object missing
// any one switch is invalid, and that the stored settings stand after the
// refusal. Each case leaves out one switch and names it in the message.
func TestProjectSettingsSetNeedsEverySwitch(t *testing.T) {
	s, st := openLocal(t)
	must(s.ProjectSettingsSet(opCtx(), core.ProjectSettingsRequest{Project: projA, Settings: core.DefaultSettings().AsInput()}))(t)
	before, err := store.GetProject(context.Background(), st, projA)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		drop func(in *core.SettingsInput)
	}{
		{"parallel_tasks", func(in *core.SettingsInput) { in.ParallelTasks = nil }},
		{"close_policy", func(in *core.SettingsInput) { in.ClosePolicy = nil }},
		{"close_policy.require_summary", func(in *core.SettingsInput) { in.ClosePolicy.RequireSummary = nil }},
		{"close_policy.require_criteria_results", func(in *core.SettingsInput) { in.ClosePolicy.RequireCriteriaResults = nil }},
		{"close_policy.require_lessons", func(in *core.SettingsInput) { in.ClosePolicy.RequireLessons = nil }},
		{"close_policy.require_mistakes", func(in *core.SettingsInput) { in.ClosePolicy.RequireMistakes = nil }},
		{"close_policy.require_no_active_steps", func(in *core.SettingsInput) { in.ClosePolicy.RequireNoActiveSteps = nil }},
	}
	for _, c := range cases {
		in := core.DefaultSettings().AsInput()
		c.drop(in)
		_, err := s.ProjectSettingsSet(opCtx(), core.ProjectSettingsRequest{Project: projA, Settings: in})
		wantKindLocal(t, err, core.KindInvalid, "settings without "+c.name)
		if err != nil && !strings.Contains(err.Error(), c.name) {
			t.Errorf("settings without %s: message %q does not name it", c.name, err.Error())
		}
	}

	after, err := store.GetProject(context.Background(), st, projA)
	if err != nil {
		t.Fatal(err)
	}
	if after.Settings != before.Settings {
		t.Errorf("settings after refused calls = %s, want %s unchanged", after.Settings, before.Settings)
	}
}

// wantKindLocal fails the test unless err is a core.Error of kind want.
func wantKindLocal(t *testing.T, err error, want core.Kind, what string) {
	t.Helper()
	if got := core.KindOf(err); got != want {
		t.Errorf("%s: error %v (kind %q), want kind %q", what, err, got, want)
	}
}
