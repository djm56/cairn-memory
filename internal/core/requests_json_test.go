package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestTaskDetailEventsKey checks the events key of a task_get result (CONTRACT
// §2). It is written as [] when the events section was read and the page is
// empty, so a client can tell no events from no section. It is left out when
// the section was not read.
func TestTaskDetailEventsKey(t *testing.T) {
	cases := []struct {
		name   string
		detail TaskDetail
		want   string // the text the JSON must contain; "" means the key is absent
	}{
		{"events read, page empty", TaskDetail{Events: []Event{}}, `"events":[]`},
		{"events read, one event", TaskDetail{Events: []Event{{ID: 1}}}, `"events":[{`},
		{"events section not read", TaskDetail{}, ""},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.detail)
		if err != nil {
			t.Fatalf("%s: marshal: %v", c.name, err)
		}
		got := string(b)
		if c.want == "" {
			if strings.Contains(got, `"events"`) {
				t.Errorf("%s: JSON %s has an events key, want none", c.name, got)
			}
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: JSON %s does not contain %s", c.name, got, c.want)
		}
	}
}
