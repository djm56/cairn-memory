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

// EventLog appends one entry to a task's journal. An agent may log note,
// blocked, unblocked, handoff, decision and custom. The kinds the calls record
// themselves are invalid here (CONTRACT §4). The entry takes the server's time
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
		return nil, Invalid("kind %q cannot be logged by an agent: use note, blocked, unblocked, handoff, decision or custom", req.Kind)
	}
	if blank(req.Text) {
		return nil, Invalid("an event needs text")
	}
	if err := checkData(req.Data); err != nil {
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
