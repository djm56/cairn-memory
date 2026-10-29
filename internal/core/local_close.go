package core

import (
	"context"
	"strings"

	"github.com/djm56/cairn-memory/internal/store"
)

// validCriterionResult reports whether r is one of the four criterion results.
func validCriterionResult(r CriterionResult) bool {
	switch r {
	case CriterionMet, CriterionPartial, CriterionUnmet, CriterionUnclear:
		return true
	}
	return false
}

// validLessonKind reports whether k is one of the three lesson kinds.
func validLessonKind(k LessonKind) bool {
	switch k {
	case LessonWrongAssumption, LessonReuse, LessonSimilarTask:
		return true
	}
	return false
}

// validImpact reports whether i is one of the three mistake impacts.
func validImpact(i Impact) bool {
	switch i {
	case ImpactLow, ImpactMed, ImpactHigh:
		return true
	}
	return false
}

// checkCloseShape checks the request of task_close on its own, before any row
// is read (CONTRACT §1.2): every enum value is known, every text that is
// required is present, and no criterion is given twice. Whether a criterion
// number exists in the task is checked later, once the task is loaded. An
// empty outcome is not checked here: the close policy reports it as missing.
func checkCloseShape(req TaskCloseRequest) error {
	if req.Outcome != "" && req.Outcome != TaskDone && req.Outcome != TaskFailed {
		return Invalid("outcome %q: a task closes as done or failed; abandon it with task_update", req.Outcome)
	}
	seen := map[int]bool{}
	for _, c := range req.Criteria {
		if seen[c.Ord] {
			return Invalid("criterion %d is given a result more than once", c.Ord)
		}
		seen[c.Ord] = true
		if !validCriterionResult(c.Result) {
			return Invalid("unknown result %q for criterion %d: use met, partial, unmet or unclear", c.Result, c.Ord)
		}
	}
	for _, l := range req.Lessons {
		if !validLessonKind(l.Kind) {
			return Invalid("unknown lesson kind %q: use wrong-assumption, reuse or similar-task", l.Kind)
		}
		if blank(l.Text) {
			return Invalid("a lesson needs its text")
		}
	}
	for _, m := range req.Mistakes {
		if blank(m.Agent) {
			return Invalid("a mistake names the agent that made it")
		}
		if blank(m.Mistake) {
			return Invalid("a mistake needs its text")
		}
		if !validImpact(m.Impact) {
			return Invalid("unknown impact %q: use low, med or high", m.Impact)
		}
	}
	for _, f := range req.Deliverables {
		if blank(f.Path) {
			return Invalid("a deliverable needs a path")
		}
	}
	return nil
}

// criteriaWithResults returns the task's criteria with the results the close
// gives. A result for a criterion number the task does not have is invalid
// (CONTRACT §2). crit is the list as read; the function changes it in place
// and returns it.
func criteriaWithResults(crit []store.CriterionRow, outcomes []CriterionOutcome) ([]store.CriterionRow, error) {
	at := map[int]int{}
	for i, c := range crit {
		at[c.Ord] = i
	}
	for _, o := range outcomes {
		i, ok := at[o.Ord]
		if !ok {
			return nil, Invalid("criterion %d is not one of this task's criteria", o.Ord)
		}
		crit[i].Result = string(o.Result)
		crit[i].Evidence = o.Evidence
	}
	return crit, nil
}

// closeMissing lists what the close policy of the project still needs, using
// the keys of CONTRACT §2. The outcome is always required. The other items
// follow the policy's switches. An absent list is not an empty one: a nil
// Lessons or Mistakes is missing, and an empty Lessons is missing unless a
// no_lessons_reason is given. The keys come in the order of the contract's table.
func closeMissing(p ClosePolicy, req TaskCloseRequest, crit []store.CriterionRow, steps []store.StepRow) []string {
	var missing []string
	if req.Outcome == "" {
		missing = append(missing, MissingOutcome)
	}
	if p.RequireSummary && blank(req.Summary) {
		missing = append(missing, MissingSummary)
	}
	if p.RequireCriteriaResults {
		for _, c := range crit {
			if c.Result == "" {
				missing = append(missing, MissingCriteria)
				break
			}
		}
	}
	if p.RequireLessons && (req.Lessons == nil || (len(req.Lessons) == 0 && blank(req.NoLessonsReason))) {
		missing = append(missing, MissingLessons)
	}
	if p.RequireMistakes && req.Mistakes == nil {
		missing = append(missing, MissingMistakes)
	}
	if p.RequireNoActiveSteps {
		for _, s := range steps {
			if s.Status == string(StepActive) {
				missing = append(missing, MissingActiveSteps)
				break
			}
		}
	}
	return missing
}

// lessonRows builds the stored lessons of task id, numbered from 1 in the order
// given (CONTRACT §2).
func lessonRows(id string, lessons []Lesson) []store.LessonRow {
	rows := make([]store.LessonRow, 0, len(lessons))
	for i, l := range lessons {
		rows = append(rows, store.LessonRow{
			TaskID: id, Ord: i + 1, Kind: string(l.Kind), Text: l.Text,
			Why: l.Why, Correction: l.Correction,
		})
	}
	return rows
}

// mistakeRows builds the stored mistakes of task id, numbered from 1 in the
// order given (CONTRACT §2).
func mistakeRows(id string, mistakes []Mistake) []store.MistakeRow {
	rows := make([]store.MistakeRow, 0, len(mistakes))
	for i, m := range mistakes {
		rows = append(rows, store.MistakeRow{
			TaskID: id, Ord: i + 1, Agent: m.Agent, Mistake: m.Mistake,
			Impact: string(m.Impact), Correction: m.Correction, Prevention: m.Prevention,
		})
	}
	return rows
}

// TaskClose ends a task as done or failed. The call checks its shape first, then
// the task (not_found), then the rules (refused): the task must be open for
// closing, which means active or blocked, and the project's close policy must
// be met. A refused close writes nothing. An accepted one writes the criteria
// results, the lessons, the mistakes and the deliverables, sets the outcome, and
// writes a close event.
func (l *Local) TaskClose(ctx context.Context, req TaskCloseRequest) (*Task, error) {
	c, err := callerFor(ctx, true)
	if err != nil {
		return nil, err
	}
	if blank(req.ID) {
		return nil, Invalid("a task id is required")
	}
	if err := checkCloseShape(req); err != nil {
		return nil, err
	}

	var out Task
	err = l.st.WithTx(ctx, func(q store.Querier) error {
		now := l.now()
		settings, err := ensureProject(ctx, q, c.Project, now)
		if err != nil {
			return err
		}
		row, err := loadTask(ctx, q, c.Project, req.ID)
		if err != nil {
			return err
		}
		crit, err := store.ListCriteria(ctx, q, row.ID)
		if err != nil {
			return err
		}
		crit, err = criteriaWithResults(crit, req.Criteria)
		if err != nil {
			return err
		}
		if err := refuseTerminal(row); err != nil {
			return err
		}
		// A queued task has not started, so it cannot be closed: CONTRACT §1.5
		// draws task_close only from active and blocked.
		if TaskStatus(row.Status) == TaskOpen {
			return Refused("task %s is open: start it before it can close", row.ID)
		}
		steps, err := store.ListSteps(ctx, q, row.ID)
		if err != nil {
			return err
		}
		if missing := closeMissing(settings.ClosePolicy, req, crit, steps); len(missing) > 0 {
			return &Error{
				Kind:    KindRefused,
				Message: "close policy not met: " + strings.Join(missing, ", "),
				Missing: missing,
			}
		}

		if err := store.ReplaceCriteria(ctx, q, row.ID, crit); err != nil {
			return err
		}
		if err := store.ReplaceLessons(ctx, q, row.ID, lessonRows(row.ID, req.Lessons)); err != nil {
			return err
		}
		if err := store.ReplaceMistakes(ctx, q, row.ID, mistakeRows(row.ID, req.Mistakes)); err != nil {
			return err
		}
		if err := store.ReplaceFiles(ctx, q, row.ID, string(FileDeliverable), fileRows(row.ID, FileDeliverable, req.Deliverables)); err != nil {
			return err
		}
		row.Status = string(req.Outcome)
		row.OutcomeSummary = req.Summary
		row.NoLessonsReason = req.NoLessonsReason
		closed := now
		row.ClosedAt = &closed
		if err := touchTask(ctx, q, &row, now); err != nil {
			return err
		}
		if err := journal(ctx, q, row, c.Agent, nil, EventClose, "Closed as "+string(req.Outcome), now); err != nil {
			return err
		}
		out = taskFromRow(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
