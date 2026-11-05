package conformance

import (
	"slices"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestUnknownDeferredNamesFail checks the rule RunWith applies to its map: a
// name that is not a case of the suite is reported, in sorted order, and a name
// that is a case is not. RunWith fails once for each name this returns.
func TestUnknownDeferredNamesFail(t *testing.T) {
	cases := []Case{{Name: "a/one"}, {Name: "a/two"}}
	deferred := map[string]string{"a/zeta": "r", "a/one": "r", "a/alpha": "r"}
	got := unknownDeferred(cases, deferred)
	if want := []string{"a/alpha", "a/zeta"}; !slices.Equal(got, want) {
		t.Errorf("unknownDeferred = %v, want %v", got, want)
	}
	if got := unknownDeferred[string](cases, nil); len(got) != 0 {
		t.Errorf("unknownDeferred with no deferrals = %v, want none", got)
	}
}

// TestProbeReportsWhetherCasePasses checks what RunWith reads from a deferred
// case: a case that passes is reported as passing, and a failing one gives each
// of its failures, in order.
func TestProbeReportsWhetherCasePasses(t *testing.T) {
	passing := Case{Name: "passes", Run: func(T, core.Service) {}}
	if passes, _ := probe(passing, core.Unimplemented{}); !passes {
		t.Errorf("a case with no checks reported as failing")
	}

	failing := Case{Name: "fails", Run: func(t T, _ core.Service) {
		t.Errorf("first problem")
		t.Errorf("second problem")
	}}
	passes, failures := probe(failing, core.Unimplemented{})
	if passes {
		t.Errorf("a failing case reported as passing")
	}
	if want := []string{"first problem", "second problem"}; !slices.Equal(failures, want) {
		t.Errorf("failures = %q, want every failure in order, %q", failures, want)
	}
}

// TestUnmatchedFailuresNamesEachOtherFault checks the rule a deferred case is held to: each failure must contain
// one of the substrings its deferral names. A failure that contains none is reported, in order, and a failure
// that contains one is not.
func TestUnmatchedFailuresNamesEachOtherFault(t *testing.T) {
	failures := []string{
		"step add: an empty after reference has no flag",
		"step update: a usage error, which carries no kind",
		"unexpected error: the store is locked",
	}
	match := []string{"has no flag", "carries no kind"}
	if got, want := unmatchedFailures(match, failures), []string{"unexpected error: the store is locked"}; !slices.Equal(got, want) {
		t.Errorf("unmatchedFailures = %q, want %q", got, want)
	}
	if got := unmatchedFailures(match, nil); len(got) != 0 {
		t.Errorf("unmatchedFailures with no failures = %q, want none", got)
	}
}

// TestMatchFaultRefusesAnEmptyMatch checks that a deferral with no substring, or with an empty one, is refused,
// since an empty substring matches every failure and would hide a different fault.
func TestMatchFaultRefusesAnEmptyMatch(t *testing.T) {
	if msg := matchFault("a/case", nil); msg == "" {
		t.Errorf("a deferral with no substring was accepted")
	}
	if msg := matchFault("a/case", []string{"cannot carry", ""}); msg == "" {
		t.Errorf("a deferral with an empty substring was accepted")
	}
	if msg := matchFault("a/case", []string{"cannot carry"}); msg != "" {
		t.Errorf("a usable deferral was refused: %q", msg)
	}
}

// TestDeferredFailureOutcomes checks the decision RunWith makes for a deferred
// case, on both outcomes. A case that passes is stale and fails the run, with
// its name in the message. A case that fails stays deferred and fails nothing.
// The outcome comes from probe, so the test runs real cases on both sides.
func TestDeferredFailureOutcomes(t *testing.T) {
	passing := Case{Name: "stale/passes", Run: func(T, core.Service) {}}
	passes, _ := probe(passing, core.Unimplemented{})
	if msg := deferredFailure(passing.Name, passes); msg != `deferred case "stale/passes" now passes: remove it from the deferred map` {
		t.Errorf("a passing deferred case gave message %q", msg)
	}

	failing := Case{Name: "live/fails", Run: func(t T, _ core.Service) { t.Errorf("not yet") }}
	passes, _ = probe(failing, core.Unimplemented{})
	if msg := deferredFailure(failing.Name, passes); msg != "" {
		t.Errorf("a failing deferred case gave message %q, want none", msg)
	}
}

// TestProbeCountsPanicAsFailure checks that a deferred case that panics is
// read as failing, so it stays deferred rather than failing the run.
func TestProbeCountsPanicAsFailure(t *testing.T) {
	panicking := Case{Name: "panics", Run: func(T, core.Service) { panic("boom") }}
	passes, failures := probe(panicking, core.Unimplemented{})
	if passes || !slices.Equal(failures, []string{"panic: boom"}) {
		t.Errorf("probe of a panicking case = %v, %q; want failing with panic: boom", passes, failures)
	}
}
