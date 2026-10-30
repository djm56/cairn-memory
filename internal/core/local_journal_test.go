package core_test

import (
	"testing"

	"github.com/djm56/cairn-memory/internal/core"
)

// TestReviewRecordRefusesNegativeCounts checks that a review's critical and
// warnings counts are not negative. DESIGN §3 says core owns this rule, as the
// schema does not state it. Each count is refused on its own, and a refused
// review writes no row.
func TestReviewRecordRefusesNegativeCounts(t *testing.T) {
	s, _ := openLocal(t)
	ctx := agent("acme-api", "main")
	res, err := s.TaskStart(ctx, core.TaskStartRequest{Title: "t", Goal: "g", Steps: []core.StepInput{{Title: "one"}}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		req  core.ReviewRecordRequest
	}{
		{"negative critical", core.ReviewRecordRequest{TaskID: res.ID, Step: core.NumberRef(1), Verdict: core.VerdictNotApproved, Critical: -1}},
		{"negative warnings", core.ReviewRecordRequest{TaskID: res.ID, Step: core.NumberRef(1), Verdict: core.VerdictNotApproved, Warnings: -1}},
	}
	for _, c := range cases {
		_, err := s.ReviewRecord(agent("acme-api", "reviewer"), c.req)
		wantKindLocal(t, err, core.KindInvalid, c.name)
	}
	d, err := s.TaskGet(ctx, core.TaskGetRequest{ID: res.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Reviews) != 0 {
		t.Errorf("reviews after refused counts = %+v, want none", d.Reviews)
	}
}
