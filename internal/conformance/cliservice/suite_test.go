package cliservice

import (
	"path/filepath"
	"testing"

	"github.com/djm56/cairn-memory/internal/conformance"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/store"
)

// Why the CLI cannot pass a case. Most cases fail on the same two facts, so they are named once.
//
//   - kind: the CLI exits 1 for every failure of a call, and prints only the message, so a check of the
//     error kind cannot pass. The Missing list of a refused close goes the same way.
//   - shape and caller: the CLI has no flag for the request, or its flag means something else, or it
//     calls as an agent named by --agent, as cairn-cli, with no session (see the adapter's package comment).
const (
	kindDropped = "the CLI exits 1 and drops the error kind"
	missingList = "the CLI exits 1 and drops the close policy's missing list"
)

// deferral is one case the CLI adapter does not pass, and why.
type deferral struct{ name, reason string }

// deferrals is the list the deferred map is made from. Each reason names what the case checks that the
// CLI cannot give.
var deferrals = []deferral{
	{"identity/viewer_first_read_is_not_found", "a viewer's read: the CLI calls as an agent only and cannot carry the viewer role"},
	{"identity/no_project_refused", kindDropped + " for an agent call with no project, which must be invalid"},
	{"identity/bad_project_name_refused", kindDropped + " for an invalid CAIRN_PROJECT, which must be invalid"},
	{"identity/agent_name_control_character_refused", "a control character in the agent name: task get, task list, learning list, directive list, recall and search do not carry the caller's agent (their --agent is absent or a role filter), so the name is not checked; task get also " + kindDropped},
	{"identity/agent_name_edge_space_refused", "an edge space in the agent name: the same reads do not carry the caller's agent, so the name is not checked; task get also " + kindDropped},
	{"identity/client_name_refused", "the caller's client name is not carried: the CLI names itself cairn-cli"},
	{"identity/session_name_refused", "the caller's session is not carried: the CLI sends no session"},

	{"task/start_requires_title_and_goal", kindDropped + " for a task with no title, no goal or an unknown priority, which must be invalid"},
	{"task/one_active_per_project", kindDropped + " for a second active task, which must be refused"},
	{"task/update_fields_and_reasons", kindDropped + " for a blocked task with no reason, done through update and an unknown task"},
	{"task/brief_frozen_once_work_starts", kindDropped + " for a brief edit after a step started, which must be refused"},
	{"task/abandoned_is_terminal", kindDropped + " for reopening an abandoned task or an event on it, which must be refused"},
	{"task/update_never_sets_open", kindDropped + " for a task_update to open, which must be refused"},
	{"task/same_status_is_refused", kindDropped + " for a task_update to the status the task has, which must be refused"},
	{"task/list_filters_and_pages", kindDropped + " for a limit over the maximum, which must be invalid"},
	{"step/insert_without_renumbering", "step add --after by id: --after takes a step number, so a step named by id cannot be carried"},
	{"step/ref_by_id_or_number", kindDropped + " for step 9 of a 2-step task, which must be not_found"},
	{"step/empty_ref_is_invalid", "step add --after an empty step reference: the CLI cannot carry an empty reference"},
	{"step/ref_gives_exactly_one", "step add --after by id, or by id 0: --after takes a step number, so these cannot be carried"},
	{"step/transitions_and_attempts", kindDropped + " for a finish on a pending step, a restart of a done step and a fail with no summary"},
	{"step/start_needs_active_task", kindDropped + " for a step start on an open task, which must be refused"},
	{"step/plan_refuses_recorded_pending_step", kindDropped + " for a step plan over a pending step with records, which must be refused"},
	{"step/plan_refusal_names_each_recorded_step", kindDropped + " for a step plan over two pending steps with records, which must be refused"},

	{"event/kinds_agents_may_log", kindDropped + " for an event kind an agent may not log, and for an event with no text, which must be invalid"},
	{"event/step_refs_must_name_a_step", "an empty step reference on event log, and task get by a step id: the CLI cannot carry either, since --step takes a number and an absent --step means the task"},
	{"event/review_data_missing_field_refused", kindDropped + " for review data with a field missing or malformed; and for data that is an array, the CLI's --data message does not name the review fix"},
	{"event/review_data_key_case_refused", kindDropped + " for review data with a key in the wrong case, which must be invalid"},
	{"event/review_data_truncated_refused", "CLI defect: a review with truncated data gets the CLI's --data check message, not the review fix that core names (CONTRACT section 4)"},
	{"event/note_data_truncated_refused", "the CLI's --data check words truncated note data as \"--data is not valid JSON\", not core's \"event data must be a JSON object\""},
	{"event/review_data_extra_key_refused", kindDropped + " for review data with an extra key, which must be invalid"},
	{"event/review_data_null_refused", kindDropped + " for review data with a null field, which must be invalid"},
	{"event/review_data_repeated_key_refused", kindDropped + " for review data with a repeated key, which must be invalid"},
	{"event/review_wrong_verdict_refused", kindDropped + " for a review verdict that is not approved or not-approved, which must be invalid"},
	{"event/review_negative_count_refused", kindDropped + " for negative review counts, which must be invalid"},
	{"event/review_on_terminal_task_refused", kindDropped + " for a review on a terminal task, which must be refused"},

	{"task_get/paging_fields_need_events", kindDropped + " for paging fields given with a section other than events, which must be invalid"},
	{"task_get/bad_paging_refused", "task get by an empty step reference, which the CLI cannot carry; " + kindDropped + " for a limit over the maximum and a cursor that does not decode"},

	{"close/policy_switch_summary", kindDropped + " and " + missingList + " for a refused close, which must be refused and name summary"},
	{"close/policy_switch_criteria", kindDropped + " and " + missingList + " for a refused close, which must be refused and name criteria"},
	{"close/policy_switch_lessons", kindDropped + " and " + missingList + " for a refused close, which must be refused and name lessons"},
	{"close/policy_switch_mistakes", kindDropped + " and " + missingList + " for a refused close, which must be refused and name mistakes"},
	{"close/policy_switch_active_steps", kindDropped + " and " + missingList + " for a refused close, which must be refused and name active_steps"},
	{"close/whole_task", kindDropped + " and " + missingList + " for a refused close; also for a second close and a step on a closed task, which must be refused"},
	{"close/empty_lists_versus_absent", kindDropped + " and " + missingList + " for a refused close with lessons or mistakes absent"},
	{"close/failed_outcome_and_validation", kindDropped + " for an abandoned outcome, a criterion that does not exist and an unknown impact, which must be invalid"},
	{"close/policy_is_project_settings", kindDropped + " for a refused close under the default policy, which must be refused"},
	{"close/refused_close_keeps_record", kindDropped + " and " + missingList + " for a refused close with no summary"},
	{"close/settings_must_be_given", kindDropped + " for a refused close under the default policy, which must be refused"},
	{"close/settings_must_be_whole", kindDropped + " for a refused close under the default policy, which must be refused"},

	{"learning/add_on_terminal_task", kindDropped + " for an event on an abandoned task, which must be refused"},
	{"learning/add_is_proposed_only", kindDropped + " for a learning with no body and one for an unknown task, which must be invalid and not_found"},
	{"learning/operator_decides", "learning list over all projects: the CLI lists one project and does not offer all_projects, by its own documented choice"},
	{"recall/across_projects", kindDropped + " for a recall with no goal, which must be invalid"},
	{"search/finds_each_kind", kindDropped + " for a search with an empty query, which must be invalid"},
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
