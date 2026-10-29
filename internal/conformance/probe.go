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

// fail records a failure, keeping the first message.
func (r *recorder) fail(f string, a ...any) {
	if !r.failed {
		r.first = fmt.Sprintf(f, a...)
	}
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

// probe runs c against s and reports whether it passed. When it did not, first
// is its first failure. RunWith uses it on a deferred case: a case that passes
// is one the deferral map should no longer name.
func probe(c Case, s core.Service) (passes bool, first string) {
	r := runCase(c, s)
	return !r.failed, r.first
}
