package conformance

import (
	"fmt"
	"runtime"

	"github.com/djm56/cairn-memory/internal/core"
)

// recorder is a T that records failures instead of reporting them. A case can
// be run and its outcome read back: the suite's own tests use it against the
// stub, and RunWith uses it to see whether a deferred case would pass.
type recorder struct {
	failed   bool
	failures []string
}

func (r *recorder) Helper()                   {}
func (r *recorder) Logf(string, ...any)       {}
func (r *recorder) Errorf(f string, a ...any) { r.fail(f, a...) }
func (r *recorder) Fatalf(f string, a ...any) {
	r.fail(f, a...)
	runtime.Goexit()
}

// fail records a failure. Every message is kept, in the order the case made them, so a deferred case can
// be checked against each of its failures.
func (r *recorder) fail(f string, a ...any) {
	r.failures = append(r.failures, fmt.Sprintf(f, a...))
	r.failed = true
}

// runCase runs c against s on its own goroutine, so Fatalf can stop it with
// Goexit as testing.T does. A panic counts as a failure.
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

// probe runs c against s and reports whether it passed. When it did not, failures
// holds each of its failure messages, in order. RunWith uses it on a deferred case:
// a case that passes is one the deferral map should no longer name, and each
// failure must be one the deferral names (unmatchedFailures).
func probe(c Case, s core.Service) (passes bool, failures []string) {
	r := runCase(c, s)
	return !r.failed, r.failures
}
