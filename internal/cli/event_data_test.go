package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/core"
	"github.com/djm56/cairn-memory/internal/identity"
)

// TestEventLogDataIsCheckedByCoreWithItsFix checks that malformed --data is refused with core's own message,
// the fix CONTRACT §4 names for a review and the message core gives for other kinds, with exit 3 and no call.
// The check runs in both forms: the text form on stderr, and the --json form as the error object on stdout.
func TestEventLogDataIsCheckedByCoreWithItsFix(t *testing.T) {
	t.Setenv(identity.EnvAgent, "")
	cases := []struct {
		name, kind, data, want string
	}{
		{"review truncated", "review", `{"verdict":`, "fix the data and log the review again"},
		{"review as an array", "review", `[1, 2, 3]`, `"not-approved"`},
		{"review missing a count", "review", `{"verdict": "approved", "critical": 0}`, `fix the missing field "warnings"`},
		{"note truncated", "note", `{"word":`, "event data must be a JSON object"},
		{"note as an array", "note", `[1]`, "event data must be a JSON object"},
	}
	for _, c := range cases {
		for _, asJSON := range []bool{false, true} {
			args := []string{"log", "task-20261030-01", "--kind", c.kind, "--text", "x", "--data", c.data, "--agent", "hicks"}
			if asJSON {
				args = append(args, "--json")
			}
			svc := &fakeWrites{event: &core.Event{}}
			code, out, errOut := runWrite(t, Event, svc, "", args...)
			msg := errOut
			if asJSON {
				var body api.ErrorBody
				if err := json.Unmarshal([]byte(out), &body); err != nil || body.Error == nil {
					t.Fatalf("%s (json): stdout %q is not the error object (%v)", c.name, out, err)
				}
				if body.Error.Kind != core.KindInvalid {
					t.Errorf("%s (json): kind %q, want invalid", c.name, body.Error.Kind)
				}
				msg = body.Error.Message
			}
			if code != 3 || svc.calls != 0 || !strings.Contains(msg, c.want) || !strings.Contains(msg, "--data") {
				t.Errorf("%s (json %v): exit %d, stdout %q, stderr %q, calls %d; want exit 3, the message naming --data and %q, and no call",
					c.name, asJSON, code, out, errOut, svc.calls, c.want)
			}
		}
	}
}
