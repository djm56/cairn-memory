package core

import (
	"context"
	"slices"
	"strings"

	"github.com/djm56/cairn-memory/internal/store"
)

// taskMoves lists the status changes task_update may make (CONTRACT §1.5).
// done and failed are not listed: task_close sets them. No move leads to open:
// a queued task is created open by task_start with queue, and nothing moves a
// task back to it.
var taskMoves = map[TaskStatus][]TaskStatus{
	TaskOpen:    {TaskActive, TaskAbandoned},
	TaskActive:  {TaskBlocked, TaskAbandoned},
	TaskBlocked: {TaskActive, TaskAbandoned},
}

// taskMayMove reports whether task_update may move a task from one status to
// another.
func taskMayMove(from, to TaskStatus) bool {
	return slices.Contains(taskMoves[from], to)
}

// activeTasks returns every active task of project, the one started last
// first. Blocked tasks do not count as active (CONTRACT §1.5). The list is read
// page by page, so a project with more active tasks than one page is counted in
// full.
//
// Read function: call it on the store, or inside a write transaction or a read
// transaction.
func activeTasks(ctx context.Context, q store.Querier, project string) ([]store.TaskRow, error) {
	var out []store.TaskRow
	var after *store.TaskKey
	for {
		page, more, err := store.ListTasks(ctx, q, project, []string{string(TaskActive)}, after, MaxLimit)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if !more || len(page) == 0 {
			break
		}
		last := page[len(page)-1]
		after = &store.TaskKey{UpdatedAt: last.UpdatedAt, ID: last.ID}
	}
	slices.SortFunc(out, startedLastFirst)
	return out, nil
}

// startedLastFirst orders tasks by when they started, newest first. A task
// with no start time sorts last.
func startedLastFirst(a, b store.TaskRow) int {
	switch {
	case a.StartedAt == nil && b.StartedAt == nil:
		return 0
	case a.StartedAt == nil:
		return 1
	case b.StartedAt == nil:
		return -1
	}
	return b.StartedAt.Compare(*a.StartedAt)
}

// checkOneActive refuses when project already has an active task other than
// except. It runs when a task is about to become active in a project that does
// not allow parallel tasks (CONTRACT §1.5). except is the task being activated,
// or empty for a new task.
//
// Read function: call it inside the write transaction that will activate.
func checkOneActive(ctx context.Context, q store.Querier, project, except string) error {
	active, err := activeTasks(ctx, q, project)
	if err != nil {
		return err
	}
	for _, t := range active {
		if t.ID != except {
			return Refused("task %s is already active in this project; block, close or abandon it first", t.ID)
		}
	}
	return nil
}

// checkBrief checks the text of a brief: every criterion and every key file
// must carry text. It is the shape check for task_start and task_update.
func checkBrief(criteria []string, files []TaskFile) error {
	for _, c := range criteria {
		if blank(c) {
			return Invalid("a criterion cannot be blank")
		}
	}
	for _, f := range files {
		if blank(f.Path) {
			return Invalid("a key file needs a path")
		}
	}
	return nil
}

// criterionRows builds the stored criteria of task id, numbered from 1 in the
// order given (CONTRACT §2).
func criterionRows(id string, criteria []string) []store.CriterionRow {
	rows := make([]store.CriterionRow, 0, len(criteria))
	for i, text := range criteria {
		rows = append(rows, store.CriterionRow{TaskID: id, Ord: i + 1, Criterion: text})
	}
	return rows
}

// fileRows builds the stored files of one role of task id, numbered from 1.
func fileRows(id string, role FileRole, files []TaskFile) []store.FileRow {
	rows := make([]store.FileRow, 0, len(files))
	for i, f := range files {
		rows = append(rows, store.FileRow{TaskID: id, Role: string(role), Ord: i + 1, Path: f.Path, Note: f.Note})
	}
	return rows
}

// TaskCurrent returns the caller's active task with its criteria, steps and the
// last five events. Task is nil when no task is active. OtherActive counts the
// rest when the project allows parallel tasks. Directives are those that apply
// to the project, as directive_list gives them with no agent.
//
// The reads run in one read transaction, so the answer is one consistent
// snapshot of the project (CONTRACT §2, §9 item 35). The store's WithTx would
// take the write lock, which a read does not need, so WithReadTx is used.
func (l *Local) TaskCurrent(ctx context.Context) (*TaskCurrent, error) {
	c, err := callerFor(ctx, false)
	if err != nil {
		return nil, err
	}
	// ensureReadProject runs before the snapshot opens: it may write a new
	// project row, which a read transaction must not do.
	if err := l.ensureReadProject(ctx, c); err != nil {
		return nil, err
	}
	cur := &TaskCurrent{}
	err = l.st.WithReadTx(ctx, func(q store.Querier) error {
		// Directives are read whether or not a task is active: they belong to the
		// project, and CONTRACT §2 gives them with every answer.
		set, err := directivesFor(ctx, q, c.Project, "", l.now())
		if err != nil {
			return err
		}
		cur.Directives = set.Directives
		cur.Expired = set.Expired
		cur.Omitted = set.Omitted
		active, err := activeTasks(ctx, q, c.Project)
		if err != nil {
			return err
		}
		if len(active) == 0 {
			return nil
		}
		// The task comes from a list scoped to the caller's project, so the child
		// reads below go by its id without GetTask. Each read is then of a row the
		// project owns, which is the scoping GetTask gives the other calls.
		row := active[0]
		t := taskFromRow(row)
		cur.Task = &t
		cur.OtherActive = len(active) - 1
		if l.betweenReads != nil {
			l.betweenReads()
		}
		crit, err := store.ListCriteria(ctx, q, row.ID)
		if err != nil {
			return err
		}
		cur.Criteria = criteriaFromRows(crit)
		steps, err := store.ListSteps(ctx, q, row.ID)
		if err != nil {
			return err
		}
		cur.Steps = stepsFromRows(steps)
		events, _, err := store.ListEvents(ctx, q, store.EventFilter{TaskID: row.ID, Project: c.Project, Limit: 5})
		if err != nil {
			return err
		}
		for _, e := range events {
			cur.RecentEvents = append(cur.RecentEvents, eventFromRow(e))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cur, nil
}

// TaskStart creates a task with its brief and plan. It is active unless
// req.Queue is set, and an active task is refused while another is active in
// a project that does not allow parallel tasks.
func (l *Local) TaskStart(ctx context.Context, req TaskStartRequest) (*TaskStartResult, error) {
	c, err := callerFor(ctx, true)
	if err != nil {
		return nil, err
	}
	if blank(req.Title) {
		return nil, Invalid("a task needs a title")
	}
	if blank(req.Goal) {
		return nil, Invalid("a task needs a goal: what done looks like")
	}
	priority := req.Priority
	if priority == "" {
		priority = PriorityNormal
	}
	if !validPriority(priority) {
		return nil, Invalid("unknown priority %q: use low, normal, high or urgent", req.Priority)
	}
	if err := checkBrief(req.Criteria, req.KeyFiles); err != nil {
		return nil, err
	}
	for _, s := range req.Steps {
		if blank(s.Title) {
			return nil, Invalid("every step needs a title")
		}
	}

	status := TaskActive
	if req.Queue {
		status = TaskOpen
	}
	var out TaskStartResult
	err = l.st.WithTx(ctx, func(q store.Querier) error {
		// The time is taken inside the transaction: a writer that waited for
		// the lock must not stamp a time earlier than a commit it followed.
		now := l.now()
		settings, err := ensureProject(ctx, q, c.Project, now)
		if err != nil {
			return err
		}
		if status == TaskActive && !settings.ParallelTasks {
			if err := checkOneActive(ctx, q, c.Project, ""); err != nil {
				return err
			}
		}
		id, err := store.AllocateTaskID(ctx, q, now)
		if err != nil {
			return err
		}
		row := store.TaskRow{
			ID: id, Project: c.Project, Title: req.Title, Goal: req.Goal,
			Context: req.Context, Constraints: req.Constraints,
			Status: string(status), Priority: string(priority),
			OpenedAt: now, UpdatedAt: now,
		}
		if status == TaskActive {
			started := now
			row.StartedAt = &started
		}
		if err := store.InsertTask(ctx, q, row); err != nil {
			return err
		}
		if err := store.ReplaceCriteria(ctx, q, id, criterionRows(id, req.Criteria)); err != nil {
			return err
		}
		if err := store.ReplaceFiles(ctx, q, id, string(FileKey), fileRows(id, FileKey, req.KeyFiles)); err != nil {
			return err
		}
		// Steps of a new task take positions 1, 2, ..., so no step is inserted
		// between two others here.
		for i, s := range req.Steps {
			_, err := store.InsertStep(ctx, q, store.StepRow{
				TaskID: id, Position: float64(i + 1), Title: s.Title, Agent: s.Agent,
				Status: string(StepPending),
			})
			if err != nil {
				return err
			}
		}
		rows, err := store.ListSteps(ctx, q, id)
		if err != nil {
			return err
		}
		out = TaskStartResult{ID: id, Status: status, Steps: stepsFromRows(rows)}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// TaskGet returns one task, and the sections asked for. Section empty returns
// all of them. The events section is one page of the journal, newest first,
// paged and filtered by step as event_list was (CONTRACT §2); the other
// sections and the empty section give the last 20 events. A task of another
// project is not_found.
func (l *Local) TaskGet(ctx context.Context, req TaskGetRequest) (*TaskDetail, error) {
	c, err := callerFor(ctx, false)
	if err != nil {
		return nil, err
	}
	if blank(req.ID) {
		return nil, Invalid("a task id is required")
	}
	switch req.Section {
	case SectionAll, SectionBrief, SectionSteps, SectionEvents, SectionClose:
	default:
		return nil, Invalid("unknown section %q: use brief, steps, events or close", req.Section)
	}
	paged := req.Section == SectionEvents
	if !paged && (req.Limit != 0 || req.Cursor != "" || req.Step != nil) {
		return nil, Invalid("limit, cursor and step apply to section events only: set section to events, or leave them out")
	}
	var size int
	var before *int64
	if paged {
		var err error
		if size, err = pageSize(req.Limit, 20); err != nil {
			return nil, err
		}
		if before, err = decodeCursor(req.Cursor); err != nil {
			return nil, err
		}
		if req.Step != nil {
			if err := checkRef(*req.Step); err != nil {
				return nil, err
			}
		}
	}
	if err := l.ensureReadProject(ctx, c); err != nil {
		return nil, err
	}
	row, err := loadTask(ctx, l.st, c.Project, req.ID)
	if err != nil {
		return nil, err
	}
	// want reports whether a section is asked for. Criteria belong to both the
	// brief and the close record, so they are read once for either.
	want := func(s TaskSection) bool { return req.Section == SectionAll || req.Section == s }
	d := &TaskDetail{Task: taskFromRow(row)}

	if want(SectionBrief) || want(SectionClose) {
		crit, err := store.ListCriteria(ctx, l.st, row.ID)
		if err != nil {
			return nil, err
		}
		d.Criteria = criteriaFromRows(crit)
	}
	if want(SectionBrief) {
		files, err := store.ListFiles(ctx, l.st, row.ID, string(FileKey))
		if err != nil {
			return nil, err
		}
		d.KeyFiles = filesFromRows(files)
	}
	if want(SectionSteps) {
		steps, err := store.ListSteps(ctx, l.st, row.ID)
		if err != nil {
			return nil, err
		}
		d.Steps = stepsFromRows(steps)
	}
	if want(SectionEvents) {
		filter := store.EventFilter{TaskID: row.ID, Project: c.Project, Before: before, Limit: 20}
		if paged {
			filter.Limit = size
			if req.Step != nil {
				st, err := resolveStep(ctx, l.st, row.ID, *req.Step)
				if err != nil {
					return nil, err
				}
				filter.StepID = &st.ID
			}
		}
		events, more, err := store.ListEvents(ctx, l.st, filter)
		if err != nil {
			return nil, err
		}
		// Non-nil even when the page is empty, so the events section is
		// written as [] and not left out (CONTRACT §2, TaskDetail.MarshalJSON).
		d.Events = make([]Event, 0, len(events))
		for _, e := range events {
			d.Events = append(d.Events, eventFromRow(e))
		}
		if paged && more && len(events) > 0 {
			d.Next = encodeCursor(events[len(events)-1].ID)
		}
	}
	if want(SectionClose) {
		lessons, err := store.ListLessons(ctx, l.st, row.ID)
		if err != nil {
			return nil, err
		}
		d.Lessons = lessonsFromRows(lessons)
		mistakes, err := store.ListMistakes(ctx, l.st, row.ID)
		if err != nil {
			return nil, err
		}
		d.Mistakes = mistakesFromRows(mistakes)
		files, err := store.ListFiles(ctx, l.st, row.ID, string(FileDeliverable))
		if err != nil {
			return nil, err
		}
		d.Deliverables = filesFromRows(files)
		d.NoLessonsReason = row.NoLessonsReason
	}
	return d, nil
}

// TaskList returns the caller's tasks, most recently updated first, filtered
// by status when any is given. Other projects' tasks are never listed.
func (l *Local) TaskList(ctx context.Context, req TaskListRequest) (*TaskList, error) {
	c, err := callerFor(ctx, false)
	if err != nil {
		return nil, err
	}
	limit, err := pageSize(req.Limit, 20)
	if err != nil {
		return nil, err
	}
	statuses := make([]string, 0, len(req.Status))
	for _, s := range req.Status {
		if !validStatus(s) {
			return nil, Invalid("unknown status %q", s)
		}
		statuses = append(statuses, string(s))
	}
	after, err := decodeTaskCursor(req.Cursor)
	if err != nil {
		return nil, err
	}
	if err := l.ensureReadProject(ctx, c); err != nil {
		return nil, err
	}
	rows, more, err := store.ListTasks(ctx, l.st, c.Project, statuses, after, limit)
	if err != nil {
		return nil, err
	}
	out := &TaskList{Tasks: make([]TaskSummary, 0, len(rows))}
	for _, r := range rows {
		out.Tasks = append(out.Tasks, taskSummaryFromRow(r))
	}
	if more {
		last := rows[len(rows)-1]
		out.Next = encodeTaskCursor(store.TaskKey{UpdatedAt: last.UpdatedAt, ID: last.ID})
	}
	return out, nil
}

// TaskUpdate changes the fields given and leaves the rest. The brief (title,
// goal, context, constraints, criteria, key files) is frozen once any step has
// started. A status change follows taskMayMove, and a move to active follows the
// one-active rule. A move to blocked or abandoned writes its reason to the
// journal, and a move from blocked to active writes an unblocked event. A status
// the task already has is not a move, so it is refused like
// any other transition that taskMayMove does not allow (CONTRACT §1.5).
func (l *Local) TaskUpdate(ctx context.Context, req TaskUpdateRequest) (*Task, error) {
	c, err := callerFor(ctx, true)
	if err != nil {
		return nil, err
	}
	if blank(req.ID) {
		return nil, Invalid("a task id is required")
	}
	if req.Status != nil {
		s := *req.Status
		switch {
		case s == TaskDone || s == TaskFailed:
			return nil, Invalid("%s is set by task_close, not task_update", s)
		case !validStatus(s):
			return nil, Invalid("unknown status %q", s)
		case (s == TaskBlocked || s == TaskAbandoned) && blank(req.Reason):
			return nil, Invalid("%s needs a reason, which goes to the journal", s)
		}
	}
	if req.Priority != nil && !validPriority(*req.Priority) {
		return nil, Invalid("unknown priority %q: use low, normal, high or urgent", *req.Priority)
	}
	if req.Title != nil && blank(*req.Title) {
		return nil, Invalid("a task needs a title")
	}
	if req.Goal != nil && blank(*req.Goal) {
		return nil, Invalid("a task needs a goal: what done looks like")
	}
	var criteria []string
	if req.Criteria != nil {
		criteria = *req.Criteria
	}
	var files []TaskFile
	if req.KeyFiles != nil {
		files = *req.KeyFiles
	}
	if err := checkBrief(criteria, files); err != nil {
		return nil, err
	}
	briefEdit := req.Title != nil || req.Goal != nil || req.Context != nil ||
		req.Constraints != nil || req.Criteria != nil || req.KeyFiles != nil

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
		if err := refuseTerminal(row); err != nil {
			return err
		}
		cur := TaskStatus(row.Status)
		if briefEdit {
			steps, err := store.ListSteps(ctx, q, row.ID)
			if err != nil {
				return err
			}
			for _, s := range steps {
				// A step has started once it has a start time: only start sets
				// one, and a step skipped from pending never gets it.
				if s.StartedAt != nil {
					return Refused("step %d has started, so the brief is frozen", s.Number)
				}
			}
		}

		next := cur
		// taskMayMove has no pair from a status to itself, so a same-status
		// request is refused below, and the reason it may carry is not dropped.
		changing := req.Status != nil
		if changing {
			next = *req.Status
			if !taskMayMove(cur, next) {
				return Refused("a task cannot go from %s to %s", cur, next)
			}
			if next == TaskActive && !settings.ParallelTasks {
				if err := checkOneActive(ctx, q, c.Project, row.ID); err != nil {
					return err
				}
			}
		}

		row.Status = string(next)
		if changing {
			switch next {
			case TaskActive:
				if row.StartedAt == nil {
					started := now
					row.StartedAt = &started
				}
			case TaskAbandoned:
				closed := now
				row.ClosedAt = &closed
			}
		}
		if req.Priority != nil {
			row.Priority = string(*req.Priority)
		}
		if req.Owner != nil {
			row.Owner = *req.Owner
		}
		if req.NextAction != nil {
			row.NextAction = *req.NextAction
		}
		if req.Blockers != nil {
			row.Blockers = *req.Blockers
		}
		if req.Title != nil {
			row.Title = *req.Title
		}
		if req.Goal != nil {
			row.Goal = *req.Goal
		}
		if req.Context != nil {
			row.Context = *req.Context
		}
		if req.Constraints != nil {
			row.Constraints = *req.Constraints
		}
		if err := touchTask(ctx, q, &row, now); err != nil {
			return err
		}
		if req.Criteria != nil {
			if err := store.ReplaceCriteria(ctx, q, row.ID, criterionRows(row.ID, criteria)); err != nil {
				return err
			}
		}
		if req.KeyFiles != nil {
			if err := store.ReplaceFiles(ctx, q, row.ID, string(FileKey), fileRows(row.ID, FileKey, files)); err != nil {
				return err
			}
		}
		if changing && (next == TaskBlocked || next == TaskAbandoned) {
			// The journal kind for an abandon is a reading: CONTRACT §1.5 says
			// the reason is written to the journal but names no kind for it.
			text, kind := strings.TrimSpace(req.Reason), EventBlocked
			if next == TaskAbandoned {
				text, kind = "Abandoned: "+text, EventDecision
			}
			if err := journal(ctx, q, row, c.Agent, nil, kind, text, now); err != nil {
				return err
			}
		}
		if changing && cur == TaskBlocked && next == TaskActive {
			// A move out of blocked is journalled in the same transaction as the
			// move (CONTRACT §1.5, §9 item 36). The request's reason is not written.
			if err := journal(ctx, q, row, c.Agent, nil, EventUnblocked, "Unblocked", now); err != nil {
				return err
			}
		}
		out = taskFromRow(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
