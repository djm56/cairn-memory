package cliservice

import (
	"path/filepath"
	"testing"

	"github.com/djm56/cairn-memory/internal/conformance"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// deferral is one case the CLI adapter does not pass, and why.
type deferral struct{ name, reason string }

// deferrals is the list the deferred map is made from. Each reason names what the case checks that the
// CLI cannot give.
var deferrals = []deferral{
	{"identity/viewer_first_read_is_not_found", "a viewer's read: the CLI calls as an agent only and cannot carry the viewer role"},
	{"identity/agent_name_control_character_refused", "a control character in the agent name: task get, task list, learning list, directive list, recall and search are not sent the caller's name, so the CLI does not check it"},
	{"identity/agent_name_edge_space_refused", "an edge space in the agent name: task get, task list, learning list, directive list, recall and search are not sent the caller's name, so the CLI does not check it"},
	{"identity/client_name_refused", "the caller's client name is not carried: the CLI names itself cairn-cli"},
	{"identity/session_name_refused", "the caller's session is not carried: the CLI sends no session"},

	{"step/insert_without_renumbering", "step add --after by id: --after takes a step number, so a step named by id cannot be carried"},
	{"step/empty_ref_is_invalid", "step add --after an empty step reference: an absent --after means last, so no flag carries an empty reference"},
	{"step/ref_gives_exactly_one", "step add --after by id, or by id 0: --after takes a step number, so these cannot be carried"},

	{"event/step_refs_must_name_a_step", "an empty step reference on event log, and task get by a step id: the CLI cannot carry either, since --step takes a number and an absent --step means the task"},
	{"event/review_data_missing_field_refused", "the CLI's --data check refuses data that is an array with its own message, which does not name the review fix that core gives"},
	{"event/review_data_truncated_refused", "CLI defect: a review with truncated data gets the CLI's --data check message, not the review fix that core names (CONTRACT section 4)"},
	{"event/note_data_truncated_refused", "the CLI's --data check words truncated note data as \"--data is not valid JSON\", not core's \"event data must be a JSON object\""},

	{"task_get/bad_paging_refused", "task get by an empty step reference: an absent --step means every step, so no flag carries an empty reference"},

	{"learning/operator_decides", "learning list over all projects: the CLI lists one project and does not offer all_projects, by its own documented choice"},
	{"web/learning_all_projects_for_readers", "learning list over all projects by an operator or a viewer: the CLI lists one project and cannot carry a reader role"},
}

// deferred is the map RunWith reads: each deferred case with its reason. RunWith fails a name that is
// not in the suite, and a case that passes, so an entry cannot outlive its gap.
var deferred = deferredMap(deferrals)

// deferredMap returns the map of list, name to reason. A name listed twice keeps the later reason, and
// TestCLIConformance fails on that, so it cannot pass unnoticed.
func deferredMap(list []deferral) map[string]string {
	m := make(map[string]string, len(list))
	for _, d := range list {
		m[d.name] = d.reason
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
