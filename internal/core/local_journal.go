package core

import (
	"context"
	"encoding/json"
	"fmt"

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

// EventList returns a task's journal, newest first, one page at a time. A step
// ref keeps only the events of that step. The default page is 20 events.
func (l *Local) EventList(ctx context.Context, req EventListRequest) (*EventList, error) {
	c, err := callerFor(ctx, false)
	if err != nil {
		return nil, err
	}
	if blank(req.TaskID) {
		return nil, Invalid("a task id is required")
	}
	if req.Step != nil {
		if err := checkRef(*req.Step); err != nil {
			return nil, err
		}
	}
	size, err := pageSize(req.Limit, 20)
	if err != nil {
		return nil, err
	}
	before, err := decodeCursor(req.Cursor)
	if err != nil {
		return nil, err
	}
	if err := l.ensureReadProject(ctx, c.Project); err != nil {
		return nil, err
	}
	task, err := loadTask(ctx, l.st, c.Project, req.TaskID)
	if err != nil {
		return nil, err
	}
	filter := store.EventFilter{TaskID: task.ID, Project: c.Project, Before: before, Limit: size}
	if req.Step != nil {
		st, err := resolveStep(ctx, l.st, task.ID, *req.Step)
		if err != nil {
			return nil, err
		}
		filter.StepID = &st.ID
	}
	rows, more, err := store.ListEvents(ctx, l.st, filter)
	if err != nil {
		return nil, err
	}
	out := &EventList{Events: []Event{}}
	for _, r := range rows {
		out.Events = append(out.Events, eventFromRow(r))
	}
	if more && len(rows) > 0 {
		out.Next = encodeCursor(rows[len(rows)-1].ID)
	}
	return out, nil
}

// ReviewRecord records a review of a step, with its verdict and counts. The
// reviewer is the calling agent, and a review event is written to the journal
// (CONTRACT §4). A step may be reviewed in any status: the contract names no
// status for a review, so the reading is that the call does not check one. A
// terminal task refuses it.
func (l *Local) ReviewRecord(ctx context.Context, req ReviewRecordRequest) (*Review, error) {
	c, err := callerFor(ctx, true)
	if err != nil {
		return nil, err
	}
	if blank(req.TaskID) {
		return nil, Invalid("a task id is required")
	}
	if err := checkRef(req.Step); err != nil {
		return nil, err
	}
	switch req.Verdict {
	case VerdictApproved, VerdictNotApproved:
	default:
		return nil, Invalid("unknown verdict %q: use approved or not-approved", req.Verdict)
	}
	if req.Critical < 0 || req.Warnings < 0 {
		return nil, Invalid("critical and warnings cannot be negative")
	}
	var out Review
	err = l.st.WithTx(ctx, func(q store.Querier) error {
		now := l.now()
		if _, err := ensureProject(ctx, q, c.Project, now); err != nil {
			return err
		}
		task, err := loadTask(ctx, q, c.Project, req.TaskID)
		if err != nil {
			return err
		}
		st, err := resolveStep(ctx, q, task.ID, req.Step)
		if err != nil {
			return err
		}
		if err := refuseTerminal(task); err != nil {
			return err
		}
		row := store.ReviewRow{
			TaskID: task.ID, StepID: st.ID, Reviewer: c.Agent, Verdict: string(req.Verdict),
			Critical: req.Critical, Warnings: req.Warnings, Summary: req.Summary, CreatedAt: now,
		}
		id, err := store.InsertReview(ctx, q, row)
		if err != nil {
			return err
		}
		row.ID = id
		text := fmt.Sprintf("Step %d reviewed: %s, %d critical, %d warnings",
			st.Number, req.Verdict, req.Critical, req.Warnings)
		stepID := st.ID
		if err := journal(ctx, q, task, c.Agent, &stepID, EventReview, text, now); err != nil {
			return err
		}
		if err := touchTask(ctx, q, &task, now); err != nil {
			return err
		}
		out = reviewFromRow(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
