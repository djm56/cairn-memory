package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// countsTask is a TaskCurrent with an active task, so the hook and status print their
// full blocks. The counts are set by each test.
func countsTask() *core.TaskCurrent {
	return &core.TaskCurrent{Task: &core.Task{ID: "task-20261031-01", Title: "Count", Status: core.TaskActive}}
}

// TestSessionStartNamesExpiredAndOmittedDirectives is 12-W2: the block the agent reads says
// how many directives were left out, and why. With none left out, the block has no such line.
func TestSessionStartNamesExpiredAndOmittedDirectives(t *testing.T) {
	cur := countsTask()
	cur.Expired, cur.Omitted = 2, 3
	var out bytes.Buffer
	writeSessionStart(&out, "acme-api", cur)
	for _, want := range []string{
		"Directives expired and left out (past review_by): 2.",
		"Directives omitted (beyond the cap of 12): 3.",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("session start = %q, want the line %q", out.String(), want)
		}
	}

	var clean bytes.Buffer
	writeSessionStart(&clean, "acme-api", countsTask())
	if strings.Contains(clean.String(), "expired") || strings.Contains(clean.String(), "omitted") {
		t.Errorf("session start with no counts = %q, want no expired or omitted line", clean.String())
	}
}

// TestStatusNamesExpiredAndOmittedDirectives is 12-W2 for cairn status: the two counts are
// printed when they are not zero, and not otherwise.
func TestStatusNamesExpiredAndOmittedDirectives(t *testing.T) {
	cur := countsTask()
	cur.Expired, cur.Omitted = 2, 3
	var out bytes.Buffer
	writeStatus(&out, &target{}, cur)
	for _, want := range []string{"expired     2 past review_by, left out", "omitted     3 beyond the cap of 12"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status = %q, want the line %q", out.String(), want)
		}
	}

	var clean bytes.Buffer
	writeStatus(&clean, &target{}, countsTask())
	if strings.Contains(clean.String(), "expired") || strings.Contains(clean.String(), "omitted") {
		t.Errorf("status with no counts = %q, want no expired or omitted line", clean.String())
	}
}
