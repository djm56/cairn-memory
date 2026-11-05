package client_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/djm56/cairn-memory/internal/api"
	"github.com/djm56/cairn-memory/internal/client"
	"github.com/djm56/cairn-memory/internal/core"
)

// TestTaskCloseSendsAbsentListsAsNullAndEmptyAsArray checks the wire form of a close, through the
// real client and the api handler. A list left out is absent, which the close policy refuses for
// lessons and mistakes, and [] says there are none (CONTRACT §2, "Absent is not empty"). The two
// must reach the server different: an absent list is null, and an empty one is []. The request
// body is captured as it reaches the handler, so the test sees what the client sent. The call's
// own answer is not the point, so it is not checked.
func TestTaskCloseSendsAbsentListsAsNullAndEmptyAsArray(t *testing.T) {
	var bodies []string
	h := api.Handler(newLocal(t), api.UnauthenticatedHeaderCaller, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read the request body: %v", err)
		}
		bodies = append(bodies, string(b))
		r.Body = io.NopCloser(bytes.NewReader(b))
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}

	absent := core.TaskCloseRequest{ID: "task-20261030-01", Outcome: core.TaskDone, Summary: "done"}
	_, _ = c.TaskClose(agentCtx(), absent)
	empty := core.TaskCloseRequest{
		ID: "task-20261030-01", Outcome: core.TaskDone, Summary: "done",
		Criteria: []core.CriterionOutcome{}, Lessons: []core.Lesson{}, Mistakes: []core.Mistake{},
	}
	_, _ = c.TaskClose(agentCtx(), empty)

	if len(bodies) != 2 {
		t.Fatalf("the server received %d requests, want 2", len(bodies))
	}
	for _, key := range []string{`"criteria":null`, `"lessons":null`, `"mistakes":null`} {
		if !strings.Contains(bodies[0], key) {
			t.Errorf("an absent list was sent as %s, not %s", bodies[0], key)
		}
	}
	for _, key := range []string{`"criteria":[]`, `"lessons":[]`, `"mistakes":[]`} {
		if !strings.Contains(bodies[1], key) {
			t.Errorf("an empty list was sent as %s, not %s", bodies[1], key)
		}
	}
}
