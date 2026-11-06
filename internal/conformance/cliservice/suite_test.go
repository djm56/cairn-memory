package cliservice

import (
	"path/filepath"
	"testing"

	"github.com/djm56/cairn-memory/internal/conformance"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// deferral is one case the CLI adapter does not pass: its name, why, and the failure text each of its failures
// must contain (conformance.Deferral).
type deferral struct {
	name   string
	reason string
	match  []string
}

// deferrals is the list the deferred map is made from. Each reason names what the case checks that the CLI
// cannot give, and each match is the text the adapter's failure for that gap carries, so a failure for any
// other reason fails the run.
//
// The three empty step reference cases and the two all_projects cases are permanent by operator decision
// (PLAN §8, D30 and D31; issues #198 and #199): the CLI will not add a flag for them.
var deferrals = []deferral{
	{
		name:   "identity/viewer_first_read_is_not_found",
		reason: "a viewer's read: the CLI calls as an agent only and cannot carry the viewer role",
		match:  []string{`cannot carry role "viewer"`},
	},
	{
		name:   "identity/client_name_refused",
		reason: "the caller's client name is not carried: the CLI names itself cairn-cli",
		match:  []string{"with the client name"},
	},
	{
		name:   "identity/session_name_refused",
		reason: "the caller's session is not carried: the CLI sends no session",
		match:  []string{"with the session"},
	},

	// Permanent: D30 (#198) for this case and the next two.
	{
		name:   "step/empty_ref_is_invalid",
		reason: "step add --after an empty step reference: an absent --after means last, so no flag carries an empty reference; and step update on number 0 is refused by the CLI as a usage error before the call, which carries no kind",
		match:  []string{"an empty after reference has no flag", "a usage error, which carries no kind"},
	},

	{
		name:   "event/step_refs_must_name_a_step",
		reason: "an empty step reference on event log and on task get: an absent --step means the task, so no flag carries an empty reference",
		match:  []string{"an empty step reference"},
	},

	{
		name:   "task_get/bad_paging_refused",
		reason: "task get by an empty step reference: an absent --step means every step, so no flag carries an empty reference",
		match:  []string{"an empty step reference"},
	},

	// Permanent: D31 (#199) for this case and the next.
	{
		name:   "learning/operator_decides",
		reason: "learning list over all projects: the CLI lists one project and does not offer all_projects, by its own documented choice",
		match:  []string{"all_projects is not offered"},
	},
	{
		name:   "web/learning_all_projects_for_readers",
		reason: "learning list over all projects by an operator or a viewer: the CLI lists one project and cannot carry a reader role",
		match:  []string{"all_projects is not offered"},
	},
}

// deferred is the map RunWith reads: each deferred case with its reason and the text its failure must contain.
// RunWith fails a name that is not in the suite, a case that passes, and a failure that matches none of its text,
// so an entry cannot outlive its gap or hide another fault.
var deferred = deferredMap(deferrals)

// deferredMap returns the map of list, name to deferral. A name listed twice keeps the later entry, and
// TestCLIConformance fails on that, so it cannot pass unnoticed.
func deferredMap(list []deferral) map[string]conformance.Deferral {
	m := make(map[string]conformance.Deferral, len(list))
	for _, d := range list {
		m[d.name] = conformance.Deferral{Reason: d.reason, Match: d.match}
	}
	return m
}

// newLocal returns a core.Local over a new database in a temporary directory.
func newLocal(t *testing.T) core.Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "cairn.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return core.NewLocal(st)
}

// TestCLIConformance runs the whole suite with every agent call made by the cairn binary, and the
// operator and web calls made over HTTP to the same server.
func TestCLIConformance(t *testing.T) {
	if len(deferred) != len(deferrals) {
		t.Errorf("the deferred list has %d entries for %d names: a name is listed twice", len(deferrals), len(deferred))
	}
	conformance.RunWith(t, func(t *testing.T) core.Service {
		return New(t, newLocal(t))
	}, deferred)
}
