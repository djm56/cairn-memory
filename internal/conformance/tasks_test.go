package conformance

import "testing"

// TestNamesStepWholeNumbers checks that namesStep matches a step number only as
// a whole number. "steps 13 and 3" names 3 and 13, and must not count as naming 1.
func TestNamesStepWholeNumbers(t *testing.T) {
	const msg = "steps 13 and 3"
	cases := []struct {
		n    int
		want bool
	}{
		{1, false}, // 1 sits inside 13 only
		{3, true},  // the bare 3
		{13, true}, // the leading 13
	}
	for _, tc := range cases {
		if got := namesStep(msg, tc.n); got != tc.want {
			t.Errorf("namesStep(%q, %d) = %v, want %v", msg, tc.n, got, tc.want)
		}
	}
}
