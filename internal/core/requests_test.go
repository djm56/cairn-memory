package core

import (
	"encoding/json"
	"testing"
)

// TestStepRefDecoding checks what a client's JSON makes of a step ref. The
// adapter decodes arguments with encoding/json, so these rows are the values a
// client can send. A null or a missing field leaves the pointer nil, which is
// "not given"; a zero that is given stays a zero. checkRef then decides whether
// the ref is valid in a journal or step call. invalid is that decision.
func TestStepRefDecoding(t *testing.T) {
	cases := []struct {
		json      string
		hasID     bool
		id        int64
		hasNumber bool
		number    int
		invalid   bool
	}{
		{json: `{}`, invalid: true},
		{json: `{"number":null}`, invalid: true},
		{json: `{"id":null,"number":3}`, hasNumber: true, number: 3, invalid: false},
		{json: `{"number":0}`, hasNumber: true, number: 0, invalid: true},
		{json: `{"id":0,"number":1}`, hasID: true, id: 0, hasNumber: true, number: 1, invalid: true},
	}
	for _, c := range cases {
		var ref StepRef
		if err := json.Unmarshal([]byte(c.json), &ref); err != nil {
			t.Fatalf("%s: decode: %v", c.json, err)
		}
		if (ref.ID != nil) != c.hasID || (c.hasID && *ref.ID != c.id) {
			t.Errorf("%s: id = %v, want given=%v value %d", c.json, ref.ID, c.hasID, c.id)
		}
		if (ref.Number != nil) != c.hasNumber || (c.hasNumber && *ref.Number != c.number) {
			t.Errorf("%s: number = %v, want given=%v value %d", c.json, ref.Number, c.hasNumber, c.number)
		}
		if got := checkRef(ref) != nil; got != c.invalid {
			t.Errorf("%s: checkRef invalid = %v (%v), want %v", c.json, got, checkRef(ref), c.invalid)
		}
	}
}
