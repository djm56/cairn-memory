package core

import (
	"context"
	"encoding/json"

	"github.com/djm56/cairn-memory/internal/store"
)

// checkData checks that the data of an event, when given, is a JSON object
// (CONTRACT §4). An absent value is no data. null, an array and a scalar are
// not objects.
func checkData(data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return Invalid("event data must be a JSON object")
	}
	return nil
}

// reviewData is the data of a review event (CONTRACT §4). The counts are
// pointers, so that a missing count is told from a zero one.
type reviewData struct {
	Verdict  *Verdict `json:"verdict"`
	Critical *int     `json:"critical"`
	Warnings *int     `json:"warnings"`
}

// checkReviewData checks the data of a review event: an object with a verdict
// of approved or not-approved and two counts of 0 or more. Each of the three is
// required. A malformed value is invalid as well, and each message names the
// fix, so the agent can log the review again without guessing.
func checkReviewData(data json.RawMessage) error {
	const shape = `a review needs data {"verdict": "approved" or "not-approved", "critical": 0, "warnings": 0}`
	var d reviewData
	if len(data) == 0 || json.Unmarshal(data, &d) != nil {
		return Invalid("%s: fix the data and log the review again", shape)
	}
	if d.Verdict == nil || d.Critical == nil || d.Warnings == nil {
		return Invalid("%s: fix the missing field and log the review again", shape)
	}
	switch *d.Verdict {
	case VerdictApproved, VerdictNotApproved:
	default:
		return Invalid("review verdict %q is not approved or not-approved: fix the verdict and log the review again", *d.Verdict)
	}
	if *d.Critical < 0 || *d.Warnings < 0 {
		return Invalid("review critical and warnings must be 0 or more: fix the counts and log the review again")
	}
	return nil
}

// EventLog appends one entry to a task's journal. An agent may log note,
// blocked, unblocked, handoff, decision, review and custom. A review needs data
// with its verdict and counts. The kinds the calls record themselves are invalid
// here (CONTRACT §4). The entry takes the server's time
// and the caller's agent, and a step ref, when given, must name a step of the
// task. A terminal task refuses it (CONTRACT §1.5).
func (l *Local) EventLog(ctx context.Context, req EventLogRequest) (*Event, error) {
	c, err := callerFor(ctx, true)
	if err != nil {
		return nil, err
	}
	if blank(req.TaskID) {
		return nil, Invalid("a task id is required")
	}
	if !req.Kind.AgentLoggable() {
		return nil, Invalid("kind %q cannot be logged by an agent: use note, blocked, unblocked, handoff, decision, review or custom", req.Kind)
	}
	if blank(req.Text) {
		return nil, Invalid("an event needs text")
	}
	// A review checks its own data, which names the fix for every fault. Other
	// kinds only need an object when they have data.
	if req.Kind == EventReview {
		if err := checkReviewData(req.Data); err != nil {
			return nil, err
		}
	} else if err := checkData(req.Data); err != nil {
		return nil, err
	}
	if req.Step != nil {
		if err := checkRef(*req.Step); err != nil {
			return nil, err
		}
	}
	var out Event
	err = l.st.WithTx(ctx, func(q store.Querier) error {
		now := l.now()
		if _, err := ensureProject(ctx, q, c.Project, now); err != nil {
			return err
		}
		task, err := loadTask(ctx, q, c.Project, req.TaskID)
		if err != nil {
			return err
		}
		var stepID *int64
		if req.Step != nil {
			st, err := resolveStep(ctx, q, task.ID, *req.Step)
			if err != nil {
				return err
			}
			id := st.ID
			stepID = &id
		}
		if err := refuseTerminal(task); err != nil {
			return err
		}
		row := store.EventRow{
			TaskID: task.ID, Project: c.Project, StepID: stepID, Agent: c.Agent,
			Kind: string(req.Kind), Text: req.Text, Data: string(req.Data), OccurredAt: now,
		}
		id, err := store.InsertEvent(ctx, q, row)
		if err != nil {
			return err
		}
		row.ID = id
		if err := touchTask(ctx, q, &task, now); err != nil {
			return err
		}
		out = eventFromRow(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
