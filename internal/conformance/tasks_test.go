package conformance

import "testing"

// TestNamesStepWholeNumbers checks that namesStep matches a step number only as
// a whole number. "steps 13 and 3" names 3 and 13, and must not count as naming 1.
// "step 13" must not count as naming 3, the suffix of 13.
func TestNamesStepWholeNumbers(t *testing.T) {
	cases := []struct {
		msg  string
		n    int
		want bool
	}{
		{"steps 13 and 3", 1, false}, // 1 sits inside 13 only
		{"steps 13 and 3", 3, true},  // the bare 3
		{"steps 13 and 3", 13, true}, // the leading 13
		{"step 13", 3, false},        // 3 is the suffix of 13
	}
	for _, tc := range cases {
		if got := namesStep(tc.msg, tc.n); got != tc.want {
			t.Errorf("namesStep(%q, %d) = %v, want %v", tc.msg, tc.n, got, tc.want)
		}
	}
}
