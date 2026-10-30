package core

import "testing"

// TestRecordedRefusal checks the step_plan refusal for one, two and three
// recorded steps (CONTRACT §3). One step takes the singular wording. Several
// steps take the plural, with the numbers joined by ", " and a final " and ",
// so every number in the message is one the caller must skip.
func TestRecordedRefusal(t *testing.T) {
	cases := []struct {
		name    string
		numbers []int
		want    string
	}{
		{
			name:    "one step uses the singular",
			numbers: []int{2},
			want:    "refused: step 2 has events or reviews, so step_plan cannot remove it: skip the step, then plan again",
		},
		{
			name:    "two steps join with and",
			numbers: []int{1, 3},
			want:    "refused: steps 1 and 3 have events or reviews, so step_plan cannot remove them: skip each step, then plan again",
		},
		{
			name:    "three steps join with a comma and a final and",
			numbers: []int{1, 2, 5},
			want:    "refused: steps 1, 2 and 5 have events or reviews, so step_plan cannot remove them: skip each step, then plan again",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := recordedRefusal(tc.numbers)
			if kind := KindOf(err); kind != KindRefused {
				t.Errorf("recordedRefusal kind = %q (%v), want refused", kind, err)
			}
			if got := err.Error(); got != tc.want {
				t.Errorf("recordedRefusal(%v) = %q, want %q", tc.numbers, got, tc.want)
			}
		})
	}
}
