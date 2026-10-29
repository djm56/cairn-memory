package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/djm56/cairn-memory/internal/store"
)

// checkRef checks the shape of a step ref: exactly one of id and number, and
// neither negative (CONTRACT §1.4). A number of 0 with no id is not a ref here.
// StepAdd gives it its one meaning, before the first step.
func checkRef(ref StepRef) error {
	switch {
	case ref.ID != 0 && ref.Number != 0:
		return Invalid("a step ref names a step by id or by number, not both")
	case ref.ID == 0 && ref.Number == 0:
		return Invalid("a step ref needs an id or a number")
	case ref.ID < 0 || ref.Number < 0:
		return Invalid("a step ref cannot be negative")
	}
	return nil
}

// refText names a ref for an error message.
func refText(ref StepRef) string {
	if ref.ID != 0 {
		return fmt.Sprintf("id %d", ref.ID)
	}
	return fmt.Sprintf("number %d", ref.Number)
}

// resolveStep returns the step a ref names in task taskID. A step of another
// task, or a number the task does not reach, is not_found. Call it after the
// task has been loaded with loadTask.
//
// Read function: call it inside the transaction that uses the step, or on the
// store for a read that needs no write.
func resolveStep(ctx context.Context, q store.Querier, taskID string, ref StepRef) (store.StepRow, error) {
	var st store.StepRow
	var err error
	if ref.ID != 0 {
		st, err = store.GetStep(ctx, q, taskID, ref.ID)
	} else {
		st, err = store.GetStepByNumber(ctx, q, taskID, ref.Number)
	}
	if errors.Is(err, store.ErrNotFound) {
		return st, NotFound("step %s not found in task %s", refText(ref), taskID)
	}
	return st, err
}

// stepSlot returns the position for a step added to task taskID. A nil after
// adds at the end. before puts the step ahead of the first one. Otherwise the
// step goes just after the step after names, and that step is resolved first.
//
// Read function: the position is only valid inside the transaction that
// inserts the step.
func stepSlot(ctx context.Context, q store.Querier, taskID string, after *StepRef, before bool) (float64, error) {
	switch {
	case after == nil:
		return store.StepPositionLast(ctx, q, taskID)
	case before:
		return store.StepPositionBetween(ctx, q, taskID, nil)
	}
	st, err := resolveStep(ctx, q, taskID, *after)
	if err != nil {
		return 0, err
	}
	return store.StepPositionBetween(ctx, q, taskID, &st.Position)
}

// StepPlan replaces the task's pending steps with the list given. Steps that
// have started are kept in their order, and the new steps follow the last kept
// step (CONTRACT §3). A pending step that an event or a review refers to cannot
// be removed, and the call is refused rather than detach the journal.
func (l *Local) StepPlan(ctx context.Context, req StepPlanRequest) (*StepList, error) {
	c, err := callerFor(ctx, true)
	if err != nil {
		return nil, err
	}
	if blank(req.TaskID) {
		return nil, Invalid("a task id is required")
	}
	for _, s := range req.Steps {
		if blank(s.Title) {
			return nil, Invalid("every step needs a title")
		}
	}
	var out StepList
	err = l.st.WithTx(ctx, func(q store.Querier) error {
		now := l.now()
		if _, err := ensureProject(ctx, q, c.Project, now); err != nil {
			return err
		}
		task, err := loadTask(ctx, q, c.Project, req.TaskID)
		if err != nil {
			return err
		}
		if err := refuseTerminal(task); err != nil {
			return err
		}
		steps, err := store.ListSteps(ctx, q, task.ID)
		if err != nil {
			return err
		}
		// One query finds the pending steps with records. The first of them, in
		// display order, names the refusal.
		recorded, err := store.PendingStepsWithRecords(ctx, q, task.ID)
		if err != nil {
			return err
		}
		if len(recorded) > 0 {
			for _, s := range steps {
				if s.ID == recorded[0] {
					return Refused("step %d has events or reviews, so step_plan cannot remove it: skip the step, then plan again", s.Number)
				}
			}
		}
		if _, err := store.DeletePendingSteps(ctx, q, task.ID); err != nil {
			return err
		}
		// After the delete, the last position is that of the last step kept.
		base, err := store.StepPositionLast(ctx, q, task.ID)
		if err != nil {
			return err
		}
		for i, s := range req.Steps {
			_, err := store.InsertStep(ctx, q, store.StepRow{
				TaskID: task.ID, Position: base + float64(i), Title: s.Title, Agent: s.Agent,
				Status: string(StepPending),
			})
			if err != nil {
				return err
			}
		}
		if err := touchTask(ctx, q, &task, now); err != nil {
			return err
		}
		rows, err := store.ListSteps(ctx, q, task.ID)
		if err != nil {
			return err
		}
		out.Steps = stepsFromRows(rows)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// StepAdd inserts one step. With no after it goes at the end; {"number": 0}
// puts it before the first step. No other step's id changes (CONTRACT §3).
//
// Go cannot tell {} from {"number": 0} in StepRef, so an After with neither id
// nor number is read as before the first step. The MCP adapter must refuse {}
// when it decodes the call. See the report on issue #9, part 1.
func (l *Local) StepAdd(ctx context.Context, req StepAddRequest) (*Step, error) {
	c, err := callerFor(ctx, true)
	if err != nil {
		return nil, err
	}
	if blank(req.TaskID) {
		return nil, Invalid("a task id is required")
	}
	if blank(req.Title) {
		return nil, Invalid("a step needs a title")
	}
	before := req.After != nil && req.After.ID == 0 && req.After.Number == 0
	if req.After != nil && !before {
		if err := checkRef(*req.After); err != nil {
			return nil, err
		}
	}
	var out Step
	err = l.st.WithTx(ctx, func(q store.Querier) error {
		now := l.now()
		if _, err := ensureProject(ctx, q, c.Project, now); err != nil {
			return err
		}
		task, err := loadTask(ctx, q, c.Project, req.TaskID)
		if err != nil {
			return err
		}
		pos, err := stepSlot(ctx, q, task.ID, req.After, before)
		if err != nil {
			return err
		}
		if err := refuseTerminal(task); err != nil {
			return err
		}
		id, err := store.InsertStep(ctx, q, store.StepRow{
			TaskID: task.ID, Position: pos, Title: req.Title, Agent: req.Agent,
			Status: string(StepPending),
		})
		if err != nil {
			return err
		}
		if err := touchTask(ctx, q, &task, now); err != nil {
			return err
		}
		st, err := store.GetStep(ctx, q, task.ID, id)
		if err != nil {
			return err
		}
		out = stepFromRow(st)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// StepUpdate starts, finishes, fails or skips a step, and writes the matching
// event. Start needs an active task. Each action has the source statuses in
// CONTRACT §3. Summary is stored when given, and Agent reassigns the step.
func (l *Local) StepUpdate(ctx context.Context, req StepUpdateRequest) (*Step, error) {
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
	switch req.Action {
	case StepStart, StepFinish, StepFail, StepSkip:
	default:
		return nil, Invalid("unknown action %q: use start, finish, fail or skip", req.Action)
	}
	if req.Action != StepStart && blank(req.Summary) {
		return nil, Invalid("%s needs a summary", req.Action)
	}
	var out Step
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
		from := StepStatus(st.Status)
		endNow := func() {
			ended := now
			st.EndedAt = &ended
		}
		var kind EventKind
		var text string
		switch req.Action {
		case StepStart:
			if from != StepPending && from != StepFailed {
				return Refused("step %d is %s; only a pending or failed step starts", st.Number, from)
			}
			if TaskStatus(task.Status) != TaskActive {
				return Refused("task %s is %s; a step starts only on an active task", task.ID, task.Status)
			}
			started := now
			st.Status = string(StepActive)
			st.Attempts++
			st.StartedAt = &started
			st.EndedAt = nil
			kind, text = EventStarted, fmt.Sprintf("Step %d started", st.Number)
		case StepFinish:
			if from != StepActive {
				return Refused("step %d is %s; only an active step finishes", st.Number, from)
			}
			st.Status = string(StepDone)
			endNow()
			kind, text = EventFinished, fmt.Sprintf("Step %d finished: %s", st.Number, req.Summary)
		case StepFail:
			if from != StepActive {
				return Refused("step %d is %s; only an active step fails", st.Number, from)
			}
			st.Status = string(StepFailed)
			endNow()
			kind, text = EventNote, fmt.Sprintf("Step %d failed: %s", st.Number, req.Summary)
		case StepSkip:
			if from != StepPending && from != StepFailed {
				return Refused("step %d is %s; only a pending or failed step is skipped", st.Number, from)
			}
			st.Status = string(StepSkipped)
			endNow()
			kind, text = EventNote, fmt.Sprintf("Step %d skipped: %s", st.Number, req.Summary)
		}
		if !blank(req.Summary) {
			st.Summary = req.Summary
		}
		if req.Agent != nil {
			st.Agent = *req.Agent
		}
		if err := store.UpdateStep(ctx, q, st); err != nil {
			return err
		}
		if err := touchTask(ctx, q, &task, now); err != nil {
			return err
		}
		id := st.ID
		if err := journal(ctx, q, task, c.Agent, &id, kind, text, now); err != nil {
			return err
		}
		out = stepFromRow(st)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
