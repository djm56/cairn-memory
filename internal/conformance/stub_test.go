package conformance

import (
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestEveryCaseFailsAgainstStub is phase 1's exit check: the suite compiles
// and every case fails against a Service that implements nothing. A case that
// passes here checks nothing.
func TestEveryCaseFailsAgainstStub(t *testing.T) {
	cases := Cases()
	if len(cases) == 0 {
		t.Fatal("the suite has no cases")
	}
	names := map[string]bool{}
	for _, c := range cases {
		if names[c.Name] {
			t.Errorf("duplicate case name %q", c.Name)
		}
		names[c.Name] = true
		r := runCase(c, core.Unimplemented{})
		if !r.failed {
			t.Errorf("%s: passed against the stub", c.Name)
			continue
		}
		t.Logf("%-45s fails: %s", c.Name, r.first)
	}
}

// TestRecorderStopsOnFatal checks the harness itself: code after Fatalf
// must not run.
func TestRecorderStopsOnFatal(t *testing.T) {
	reached := false
	r := runCase(Case{Name: "fatal", Run: func(t T, _ core.Service) {
		t.Fatalf("stop")
		reached = true
	}}, core.Unimplemented{})
	if !r.failed || reached {
		t.Errorf("failed = %v, reached after Fatalf = %v", r.failed, reached)
	}
}
