package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/auth"
	"github.com/djm56/cairn-memory/internal/conformance"
	"github.com/djm56/cairn-memory/internal/core"

	// cclient is the client package under its own name: client is taken by a helper of
	// server_test.go.
	cclient "github.com/djm56/cairn-memory/internal/client"
)

// The M2 exit checks (docs/PLAN.md §3, M2), run against a real listener. Each test starts
// Run on 127.0.0.1:0, as cairn serve does, and makes its keys with AddKey, as cairn key add
// does. No test here uses httptest.

// listeningAddr matches the listening line Run writes once its listener is bound. The
// address is the one the listener reports, so a test reaches the listener Run bound.
var listeningAddr = regexp.MustCompile(`msg=listening addr=(\S+)`)

// wrongShape is a body that is not the request of any call. The wire gate refuses a call
// by role before it reads the body (CONTRACT §1.2), so the refusal is the answer; a gate
// that was not there would decode the body and answer 400. The loops send it so that they
// see the gate itself, not the core's own check of the same role.
const wrongShape = `{"no_such_field":1}`

// realListener starts Run on 127.0.0.1:0 over the database at db, and returns the base URL of
// the listener Run bound, read from its listening line. The server is stopped at the end of
// the test, and the test fails if Run does not return nil.
func realListener(t *testing.T, db string) string {
	t.Helper()
	var logs syncBuffer
	ctx, cancel := context.WithCancel(context.Background())
	var runErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		runErr = Run(ctx, Options{Addr: "127.0.0.1:0", DB: db, Log: &logs})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
			if runErr != nil {
				t.Errorf("Run returned %v after the stop, want nil", runErr)
			}
		case <-time.After(15 * time.Second):
			t.Error("Run did not return within 15s of the stop")
		}
	})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if m := listeningAddr.FindStringSubmatch(logs.String()); m != nil {
			return "http://" + m[1]
		}
		select {
		case <-done:
			t.Fatalf("Run returned before it listened: %v; log %q", runErr, logs.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("no listening line within 10s; log %q", logs.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// mustAddKey makes a key named name with scope in the database at db, through AddKey, and
// returns its plaintext.
func mustAddKey(t *testing.T, db, name string, scope auth.Scope) string {
	t.Helper()
	key, err := AddKey(context.Background(), db, name, scope)
	if err != nil {
		t.Fatalf("AddKey %s: %v", name, err)
	}
	return key
}

// writeRoutes is every write route of the route table, by its path. It is written out here and
// not read from the table, so a route whose mark is flipped is a mismatch that fails the test,
// and does not move silently to the other side of a loop. routeClasses checks it against the
// table in both directions: a route the table marks write that is not listed, and a listed route
// the table does not mark write, each fail.
var writeRoutes = []string{
	"/agent/task_start", "/agent/task_update", "/agent/task_close",
	"/agent/step_plan", "/agent/step_add", "/agent/step_update",
	"/agent/event_log", "/agent/learning_add",
	"/operator/learning_decide", "/operator/directive_ratify", "/operator/directive_retire",
	"/operator/project_settings_set",
}

// operatorPrefix is the path every operator call lives under (CONTRACT §8, api.go). The
// operator routes are the routes under it, and routeClasses checks the table's Operator mark
// against the prefix in both directions.
const operatorPrefix = "/operator/"

// routeClasses returns the write routes and the read routes of the route table, and the
// operator routes, which are the table's and agree with the prefix. Each disagreement between
// the table and the independent lists is an error of t, so a flipped mark fails the test that
// uses the result. It fails when the table has no write route or no read route.
func routeClasses(t *testing.T) (writes, reads, operators []api.RouteInfo) {
	t.Helper()
	table := api.Routes()
	inTable := map[string]bool{}
	for _, r := range table {
		inTable[r.Route] = true
	}
	for _, want := range writeRoutes {
		if !inTable[want] {
			t.Errorf("%s is in the write list but not in the route table", want)
		}
	}
	for _, r := range table {
		isWrite := slices.Contains(writeRoutes, r.Route)
		if isWrite != r.Write {
			t.Errorf("%s: the table marks write=%v, the write list says %v", r.Route, r.Write, isWrite)
		}
		isOperator := strings.HasPrefix(r.Route, operatorPrefix)
		if isOperator != r.Operator {
			t.Errorf("%s: the table marks operator=%v, the operator prefix says %v", r.Route, r.Operator, isOperator)
		}
		if isWrite {
			writes = append(writes, r)
		} else {
			reads = append(reads, r)
		}
		if isOperator {
			operators = append(operators, r)
		}
	}
	if len(writes) == 0 || len(reads) == 0 {
		t.Fatalf("the route table has %d write and %d read routes: both loops need both", len(writes), len(reads))
	}
	return writes, reads, operators
}

// errorKind returns the kind of an error body, or "" when body is not one.
func errorKind(body []byte) core.Kind {
	var eb api.ErrorBody
	if err := json.Unmarshal(body, &eb); err != nil || eb.Error == nil {
		return ""
	}
	return eb.Error.Kind
}

// routedService is a core.Service over one client per key. A call goes to the client whose key
// carries the role of the call's caller, so an operator call arrives with the operator key and a
// viewer call with the viewer key. The key gives the role, not the Cairn headers, so the
// server's own gate decides each call.
type routedService struct {
	agent, operator, viewer core.Service
}

// pick returns the client for the role ctx carries: operator and viewer have their own, and
// every other caller uses the agent key.
func (r routedService) pick(ctx context.Context) core.Service {
	c, _ := core.CallerFrom(ctx)
	switch c.Role {
	case core.RoleOperator:
		return r.operator
	case core.RoleViewer:
		return r.viewer
	}
	return r.agent
}

func (r routedService) TaskCurrent(ctx context.Context) (*core.TaskCurrent, error) {
	return r.pick(ctx).TaskCurrent(ctx)
}

func (r routedService) TaskStart(ctx context.Context, req core.TaskStartRequest) (*core.TaskStartResult, error) {
	return r.pick(ctx).TaskStart(ctx, req)
}

func (r routedService) TaskGet(ctx context.Context, req core.TaskGetRequest) (*core.TaskDetail, error) {
	return r.pick(ctx).TaskGet(ctx, req)
}

func (r routedService) TaskList(ctx context.Context, req core.TaskListRequest) (*core.TaskList, error) {
	return r.pick(ctx).TaskList(ctx, req)
}

func (r routedService) TaskUpdate(ctx context.Context, req core.TaskUpdateRequest) (*core.Task, error) {
	return r.pick(ctx).TaskUpdate(ctx, req)
}

func (r routedService) TaskClose(ctx context.Context, req core.TaskCloseRequest) (*core.Task, error) {
	return r.pick(ctx).TaskClose(ctx, req)
}

func (r routedService) StepPlan(ctx context.Context, req core.StepPlanRequest) (*core.StepList, error) {
	return r.pick(ctx).StepPlan(ctx, req)
}

func (r routedService) StepAdd(ctx context.Context, req core.StepAddRequest) (*core.Step, error) {
	return r.pick(ctx).StepAdd(ctx, req)
}

func (r routedService) StepUpdate(ctx context.Context, req core.StepUpdateRequest) (*core.Step, error) {
	return r.pick(ctx).StepUpdate(ctx, req)
}

func (r routedService) EventLog(ctx context.Context, req core.EventLogRequest) (*core.Event, error) {
	return r.pick(ctx).EventLog(ctx, req)
}

func (r routedService) LearningAdd(ctx context.Context, req core.LearningAddRequest) (*core.Learning, error) {
	return r.pick(ctx).LearningAdd(ctx, req)
}

func (r routedService) LearningList(ctx context.Context, req core.LearningListRequest) (*core.LearningList, error) {
	return r.pick(ctx).LearningList(ctx, req)
}

func (r routedService) DirectiveList(ctx context.Context, req core.DirectiveListRequest) (*core.DirectiveList, error) {
	return r.pick(ctx).DirectiveList(ctx, req)
}

func (r routedService) Recall(ctx context.Context, req core.RecallRequest) (*core.RecallResult, error) {
	return r.pick(ctx).Recall(ctx, req)
}

func (r routedService) Search(ctx context.Context, req core.SearchRequest) (*core.SearchResult, error) {
	return r.pick(ctx).Search(ctx, req)
}

func (r routedService) LearningDecide(ctx context.Context, req core.LearningDecideRequest) (*core.Learning, error) {
	return r.pick(ctx).LearningDecide(ctx, req)
}

func (r routedService) DirectiveRatify(ctx context.Context, req core.DirectiveRatifyRequest) (*core.Directive, error) {
	return r.pick(ctx).DirectiveRatify(ctx, req)
}

func (r routedService) DirectiveRetire(ctx context.Context, req core.DirectiveRetireRequest) (*core.Directive, error) {
	return r.pick(ctx).DirectiveRetire(ctx, req)
}

func (r routedService) ProjectSettingsSet(ctx context.Context, req core.ProjectSettingsRequest) (*core.Project, error) {
	return r.pick(ctx).ProjectSettingsSet(ctx, req)
}

// listenerService returns the suite's Service over a fresh database and a real listener: one
// agent, one operator and one viewer key, each client sending its own.
func listenerService(t *testing.T) core.Service {
	t.Helper()
	db := filepath.Join(t.TempDir(), "cairn.db")
	agent := mustAddKey(t, db, "exit-agent", auth.ScopeAgent)
	operator := mustAddKey(t, db, "exit-operator", auth.ScopeOperator)
	viewer := mustAddKey(t, db, "exit-viewer", auth.ScopeViewer)
	base := realListener(t, db)
	return routedService{
		agent:    listenerClient(t, base, agent),
		operator: listenerClient(t, base, operator),
		viewer:   listenerClient(t, base, viewer),
	}
}

// listenerClient returns the client of base that sends key on every call.
func listenerClient(t *testing.T, base, key string) core.Service {
	t.Helper()
	c, err := cclient.NewWithKey(base, key, nil)
	if err != nil {
		t.Fatalf("NewWithKey: %v", err)
	}
	return c
}

// TestConformanceOverRealListener runs the whole suite through internal/client, against a real
// listener that Run binds on 127.0.0.1:0, with the agent, operator and viewer keys the suite's
// calls need. Each case gets its own database, keys and listener. The httptest runner in
// internal/client is kept: this one proves the same suite over the server cairn serve runs.
func TestConformanceOverRealListener(t *testing.T) {
	conformance.Run(t, listenerService)
}

// forged returns the call headers with X-Cairn-Role set to role, as a client sends when it
// claims a role its key does not hold (10-W2). The server must take the role from the key,
// so a forged header changes nothing. The header is set on a copy, so h is not changed.
func forged(h map[string]string, role string) map[string]string {
	out := map[string]string{api.HeaderRole: role}
	for k, v := range h {
		out[k] = v
	}
	return out
}

// TestAgentKeyIsRefusedOnEveryOperatorRoute is the exit check that an agent key is refused on
// operator routes. The routes come from the route table, through api.Routes, not from a list
// here. Each operator route must answer 403 operator_only to an agent key, with a body that
// does not hold the key. An operator key is not refused on the same routes for its role, so the
// refusal is the gate's and not a broken route.
func TestAgentKeyIsRefusedOnEveryOperatorRoute(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cairn.db")
	agent := mustAddKey(t, db, "exit-agent", auth.ScopeAgent)
	operator := mustAddKey(t, db, "exit-operator", auth.ScopeOperator)
	base := realListener(t, db)
	c := client(nil)
	_, _, operators := routeClasses(t)
	for _, r := range operators {
		resp, body := send(t, c, http.MethodPost, base+api.Prefix+r.Route, wrongShape, callHeaders(agent))
		if resp.StatusCode != http.StatusForbidden || errorKind(body) != core.KindOperatorOnly {
			t.Errorf("%s with an agent key: status %d, body %s; want 403 operator_only", r.Route, resp.StatusCode, body)
		}
		if strings.Contains(string(body), agent) {
			t.Errorf("%s: the refusal to an agent key holds the key", r.Route)
		}
		// The same agent key, claiming the operator role in a header: still refused.
		resp, body = send(t, c, http.MethodPost, base+api.Prefix+r.Route, wrongShape, forged(callHeaders(agent), "operator"))
		if resp.StatusCode != http.StatusForbidden || errorKind(body) != core.KindOperatorOnly {
			t.Errorf("%s with an agent key and X-Cairn-Role: operator: status %d, body %s; want 403 operator_only", r.Route, resp.StatusCode, body)
		}
		resp, body = send(t, c, http.MethodPost, base+api.Prefix+r.Route, wrongShape, callHeaders(operator))
		if resp.StatusCode == http.StatusForbidden && errorKind(body) == core.KindOperatorOnly {
			t.Errorf("%s with an operator key: refused as operator_only, the control call is refused too", r.Route)
		}
		if strings.Contains(string(body), operator) {
			t.Errorf("%s: the answer to an operator key holds the key", r.Route)
		}
	}
	if len(operators) == 0 {
		t.Fatal("the route table has no operator route: the loop checked nothing")
	}
}

// TestViewerKeyIsRefusedOnEveryWriteRoute is the exit check of issue #73: a viewer key is
// refused on every write route with 403 read_only, and is not refused for its scope on any read
// route. Both sets come from api.Routes, and each iteration checks its own answer and that the
// answer does not hold the key.
func TestViewerKeyIsRefusedOnEveryWriteRoute(t *testing.T) {
	db := filepath.Join(t.TempDir(), "cairn.db")
	viewer := mustAddKey(t, db, "exit-viewer", auth.ScopeViewer)
	base := realListener(t, db)
	c := client(nil)
	writes, reads, _ := routeClasses(t)
	for _, r := range writes {
		resp, body := send(t, c, http.MethodPost, base+api.Prefix+r.Route, wrongShape, callHeaders(viewer))
		if resp.StatusCode != http.StatusForbidden || errorKind(body) != api.KindReadOnly {
			t.Errorf("%s (write) with a viewer key: status %d, body %s; want 403 read_only", r.Route, resp.StatusCode, body)
		}
		if strings.Contains(string(body), viewer) {
			t.Errorf("%s: the refusal to a viewer key holds the key", r.Route)
		}
		// The viewer key claiming the operator, then the agent role in a header: both refused.
		for _, role := range []string{"operator", "agent"} {
			resp, body = send(t, c, http.MethodPost, base+api.Prefix+r.Route, wrongShape, forged(callHeaders(viewer), role))
			if resp.StatusCode != http.StatusForbidden || errorKind(body) != api.KindReadOnly {
				t.Errorf("%s (write) with a viewer key and X-Cairn-Role: %s: status %d, body %s; want 403 read_only", r.Route, role, resp.StatusCode, body)
			}
		}
	}
	for _, r := range reads {
		resp, body := send(t, c, http.MethodPost, base+api.Prefix+r.Route, wrongShape, callHeaders(viewer))
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
			t.Errorf("%s (read) with a viewer key: status %d, body %s; a read route must not refuse a viewer for its scope", r.Route, resp.StatusCode, body)
		}
		// A read that reaches its handler answers with its own result or a refusal of its
		// body, never no_route (the path is in the table) or internal (the handler failed).
		if kind := errorKind(body); kind == api.KindNoRoute || kind == api.KindInternal {
			t.Errorf("%s (read) with a viewer key: kind %q; a read route must reach its handler", r.Route, kind)
		}
		if strings.Contains(string(body), viewer) {
			t.Errorf("%s: the answer to a viewer key holds the key", r.Route)
		}
	}
}
