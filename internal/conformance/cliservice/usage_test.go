package cliservice

import (
	"errors"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestUsageNamesFlagCountsOnlyTheValuesTheCallSet checks the rule that decides whether a usage error (exit 2)
// is read as core's KindInvalid. It is counted when its message names a flag the call set, or is a refusal of
// the caller's name. It is not counted when the flag package rejected the call's own arguments, or when the
// message names a flag the call did not set, or is about a positional argument.
func TestUsageNamesFlagCountsOnlyTheValuesTheCallSet(t *testing.T) {
	cases := []struct {
		name   string
		argv   []string
		stderr string
		want   bool
	}{
		{"a value the call set is named",
			[]string{"step", "add", "task-1", "--after-id", "0", "--json"},
			"cairn step add: --after-id must be a step id, 1 or more; leave it out to add the step last", true},
		{"two forms of one position are named",
			[]string{"task", "get", "task-1", "--step", "1", "--step-id", "4", "--json"},
			"cairn task get: give the step by --step or by --step-id, not both", true},
		{"a refused caller name from the variable",
			[]string{"task", "get", "task-1", "--json"},
			"cairn task get: the name from CAIRN_AGENT is refused: the agent name \"ma\\nin\" has a control character", true},
		{"no caller name at all",
			[]string{"task", "start", "--title", "t", "--json"},
			"cairn task start: no agent name: give --agent <name>, or set CAIRN_AGENT to one", true},
		{"the flag package refused the call's own arguments",
			[]string{"task", "get", "task-1", "--bogus", "--json"},
			"cairn task get: flag provided but not defined: -bogus; run cairn task get -h for the flags", false},
		{"a flag the call did not set is named",
			[]string{"step", "add", "task-1", "--after", "1", "--json"},
			"cairn step add: --step must be a step number, 1 or more", false},
		{"a positional argument is refused",
			[]string{"step", "update", "task-1", "0", "--action", "start", "--json"},
			"cairn step update: the step is a number, 1 or more, as the steps list shows it: got \"0\"", false},
	}
	for _, c := range cases {
		if got := usageNamesFlag(c.argv, c.stderr); got != c.want {
			t.Errorf("%s: usageNamesFlag = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestUsageErrorIsInvalidOnlyWhenItNamesAValue checks what failure does with exit 2: a usage error that names a
// value the call set is core's KindInvalid, and one that names none is a plain error with no kind, so a case
// that expects a kind cannot pass on it.
func TestUsageErrorIsInvalidOnlyWhenItNamesAValue(t *testing.T) {
	argv := []string{"step", "add", "task-1", "--after-id", "0", "--json"}
	named := failure(2, argv, nil, "cairn step add: --after-id must be a step id, 1 or more\n")
	var ce *core.Error
	if !errors.As(named, &ce) || ce.Kind != core.KindInvalid {
		t.Errorf("a usage error naming the value gave %v, want core's KindInvalid", named)
	}
	unnamed := failure(2, argv, nil, "cairn step update: the step is a number, 1 or more\n")
	if errors.As(unnamed, &ce) {
		t.Errorf("a usage error naming no value gave the kind %q, want no kind", ce.Kind)
	}
}
