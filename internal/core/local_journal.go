package core

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"slices"

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

// reviewKeys are the only keys of review data, spelled exactly (CONTRACT §4).
var reviewKeys = []string{"critical", "verdict", "warnings"}

// checkReviewData checks the data of a review event: a JSON object with exactly
// the keys verdict, critical and warnings, spelled as they are here. The verdict
// is approved or not-approved, and each count is 0 or more. A wrong key case, an
// extra key, a repeated key and null are invalid. So is a malformed value, such
// as a count as text or a fraction. Each message names the fix, so the agent can
// log the review again without guessing.
func checkReviewData(data json.RawMessage) error {
	const shape = `a review needs data {"verdict": "approved" or "not-approved", "critical": 0, "warnings": 0}`
	if len(data) == 0 {
		return Invalid("%s: fix the data and log the review again", shape)
	}
	// encoding/json matches struct fields without regard to case, so the keys
	// are checked as a map first. Its decode keeps the last of a repeated key,
	// so repeats are checked separately.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return Invalid("%s: fix the data and log the review again", shape)
	}
	if key, ok := repeatedKey(data); ok {
		return Invalid("review data repeats the key %q: give each key once and log the review again", key)
	}
	for _, key := range slices.Sorted(maps.Keys(obj)) {
		if !slices.Contains(reviewKeys, key) {
			return Invalid("review data has the key %q, which is not verdict, critical or warnings: remove it and log the review again", key)
		}
	}
	for _, key := range reviewKeys {
		if v, ok := obj[key]; !ok || string(v) == "null" {
			return Invalid("%s: fix the missing field %q and log the review again", shape, key)
		}
	}
	var d reviewData
	if err := json.Unmarshal(data, &d); err != nil {
		return Invalid("%s: fix the malformed value and log the review again", shape)
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

// repeatedKey returns the first key that data, a JSON object, gives more than
// once at its top level, and true. It returns "", false when no key repeats, or
// when data is not an object. It reads tokens, because encoding/json does not
// report a repeated key.
func repeatedKey(data json.RawMessage) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return "", false
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return "", false
		}
		key, _ := tok.(string)
		if seen[key] {
			return key, true
		}
		seen[key] = true
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return "", false
		}
	}
	return "", false
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
