// Package conformance is the behaviour suite every core.Service must pass
// (docs/DESIGN.md §5.1, docs/PLAN.md phase 1). A behaviour exists when this
// suite checks it.
//
// A backend's test calls Run with a constructor that returns a fresh, empty
// Service for each case:
//
//	func TestConformance(t *testing.T) {
//		conformance.Run(t, func(t *testing.T) core.Service { return newLocal(t) })
//	}
package conformance

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// T is the part of *testing.T the cases use. It is an interface so that the
// suite's own test can run every case against a recorder (see
// stub_test.go). Fatalf must not return.
type T interface {
	Helper()
	Logf(format string, args ...any)
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// Case is one behaviour. Run gets a fresh, empty Service.
type Case struct {
	Name string
	Run  func(t T, s core.Service)
}

// Run runs every case as a subtest against a fresh Service from newService.
func Run(t *testing.T, newService func(t *testing.T) core.Service) {
	RunWith(t, newService, nil)
}

// RunWith is Run for a backend that does not yet implement the whole suite.
// Each case named in deferred is reported as skipped, with the reason its
// backend gave, so it counts as neither a pass nor a fail. Two things fail the
// run. A name that is not in the suite fails it, so a renamed case cannot be
// skipped by accident. A deferred case that passes against the backend fails
// it too, so a stale entry cannot hide a case that is now live. A log line
// would not do: a green run does not read its log, and the entry would stay.
func RunWith(t *testing.T, newService func(t *testing.T) core.Service, deferred map[string]string) {
	cases := Cases()
	for _, name := range unknownDeferred(cases, deferred) {
		t.Errorf("deferred case %q is not in the suite", name)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			reason, ok := deferred[c.Name]
			if !ok {
				c.Run(t, newService(t))
				return
			}
			// The case runs on a fresh service and its outcome is only read.
			passes, _ := probe(c, newService(t))
			if msg := deferredFailure(c.Name, passes); msg != "" {
				t.Error(msg)
			}
			t.Skip("deferred: " + reason)
		})
	}
}

// deferredFailure is the failure RunWith reports for a deferred case, given
// whether the case passed against the backend. A case that passes is stale: the
// deferred map must drop it, so the message names it. A case that still fails
// stays deferred, and the result is "".
func deferredFailure(name string, passes bool) string {
	if !passes {
		return ""
	}
	return fmt.Sprintf("deferred case %q now passes: remove it from the deferred map", name)
}

// unknownDeferred returns the names in deferred that no case of cases has, in
// sorted order. RunWith fails for each of them.
func unknownDeferred(cases []Case, deferred map[string]string) []string {
	known := map[string]bool{}
	for _, c := range cases {
		known[c.Name] = true
	}
	var unknown []string
	for name := range deferred {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	return unknown
}

// Cases returns the whole suite, in a fixed order.
func Cases() []Case {
	var all []Case
	all = append(all, identityCases...)
	all = append(all, taskCases...)
	all = append(all, stepCases...)
	all = append(all, eventCases...)
	all = append(all, closeCases...)
	all = append(all, learningCases...)
	all = append(all, directiveCapCases...)
	all = append(all, searchCases...)
	return all
}

// Names used across cases. Each case gets a fresh Service, so they never
// collide between cases.
const (
	projA    = "acme-api"
	projB    = "acme-web"
	operator = "operator"
)

// as returns a context for agent in project.
func as(project, agent string) context.Context {
	return core.WithCaller(context.Background(), core.Caller{
		Project: project, Agent: agent, Client: "conformance", Role: core.RoleAgent,
	})
}

// mainCtx is the usual caller: agent "main" in projA.
func mainCtx() context.Context { return as(projA, "main") }

// op returns an operator's context. Operators act across projects.
func op() context.Context {
	return core.WithCaller(context.Background(), core.Caller{Agent: operator, Role: core.RoleOperator})
}

// must takes a call's results and returns a check: must(s.TaskGet(...))(t)
// fails the case when the call returned an error, and returns its value
// otherwise. (Go cannot pass a multi-value call next to another argument.)
func must[V any](v V, err error) func(T) V {
	return func(t T) V {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return v
	}
}

// wantKind fails the case unless err is a core.Error of kind want.
func wantKind(t T, err error, want core.Kind, what string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: got no error, want %s", what, want)
		return
	}
	if got := core.KindOf(err); got != want {
		t.Errorf("%s: got error %v (kind %q), want kind %q", what, err, got, want)
	}
}

func ptr[V any](v V) *V { return &v }

// startTask starts an active task, without criteria, with one step per
// title.
func startTask(t T, s core.Service, ctx context.Context, title string, steps ...string) *core.TaskStartResult {
	t.Helper()
	req := core.TaskStartRequest{Title: title, Goal: "Goal of " + title}
	for _, st := range steps {
		req.Steps = append(req.Steps, core.StepInput{Title: st})
	}
	return must(s.TaskStart(ctx, req))(t)
}

// stepTitles returns the titles of steps in their displayed order, checking
// that Number runs 1..n.
func stepTitles(t T, steps []core.Step) []string {
	t.Helper()
	titles := make([]string, len(steps))
	for i, st := range steps {
		if st.Number != i+1 {
			t.Errorf("step %q has number %d at position %d", st.Title, st.Number, i+1)
		}
		titles[i] = st.Title
	}
	return titles
}

// fullClose is a close request that meets the default policy for a task
// with n criteria.
func fullClose(id string, n int) core.TaskCloseRequest {
	req := core.TaskCloseRequest{
		ID:       id,
		Outcome:  core.TaskDone,
		Summary:  "Did the thing.",
		Lessons:  []core.Lesson{{Kind: core.LessonReuse, Text: "Reuse the parser.", Why: "It already handled the edge cases."}},
		Mistakes: []core.Mistake{},
	}
	for i := 1; i <= n; i++ {
		req.Criteria = append(req.Criteria, core.CriterionOutcome{Ord: i, Result: core.CriterionMet, Evidence: "test passes"})
	}
	return req
}
