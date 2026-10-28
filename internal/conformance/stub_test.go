package conformance

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// recorder is a T that records failures instead of reporting them, so a
// case can be run and expected to fail.
type recorder struct {
	failed bool
	first  string
}

func (r *recorder) Helper()                   {}
func (r *recorder) Logf(string, ...any)       {}
func (r *recorder) Errorf(f string, a ...any) { r.fail(f, a...) }
func (r *recorder) Fatalf(f string, a ...any) {
	r.fail(f, a...)
	runtime.Goexit()
}

func (r *recorder) fail(f string, a ...any) {
	if !r.failed {
		r.first = fmt.Sprintf(f, a...)
	}
	r.failed = true
}

// runCase runs c against s on its own goroutine, so Fatalf can stop it
// with Goexit as testing.T does. A panic counts as a failure.
func runCase(c Case, s core.Service) *recorder {
	r := &recorder{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if p := recover(); p != nil {
				r.fail("panic: %v", p)
			}
		}()
		c.Run(r, s)
	}()
	<-done
	return r
}

// TestEveryCaseFailsAgainstStub is phase 1's exit check: the suite compiles
// and every case fails against a Service that implements nothing. A case
// that passes here checks nothing.
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
