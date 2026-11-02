package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// t0 is the fixed clock of the query tests. Rows get later times by adding to it.
var t0 = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

// seedProjects creates the named projects with default settings at t0.
func seedProjects(t *testing.T, s *Store, names ...string) {
	t.Helper()
	for _, n := range names {
		err := EnsureProject(context.Background(), s, ProjectRow{
			Name: n, CreatedAt: t0, LastSeenAt: t0, Settings: `{"parallel_tasks":false}`,
		})
		if err != nil {
			t.Fatalf("EnsureProject %s: %v", n, err)
		}
	}
}

// newTask returns an open task of project with the given id, updated at at.
func newTask(id, project string, at time.Time) TaskRow {
	return TaskRow{
		ID: id, Project: project, Title: "title " + id, Goal: "goal " + id,
		Status: "open", Priority: "normal", OpenedAt: at, UpdatedAt: at,
	}
}

// insertTask writes one task inside a transaction and fails the test on error.
func insertTask(t *testing.T, s *Store, task TaskRow) {
	t.Helper()
	err := s.WithTx(context.Background(), func(q Querier) error {
		return InsertTask(context.Background(), q, task)
	})
	if err != nil {
		t.Fatalf("InsertTask %s: %v", task.ID, err)
	}
}

// insertStepAt writes a step at the position StepPositionLast gives, returning
// its id.
func insertStepAt(t *testing.T, s *Store, taskID, title, status string) int64 {
	t.Helper()
	var id int64
	err := s.WithTx(context.Background(), func(q Querier) error {
		pos, err := StepPositionLast(context.Background(), q, taskID)
		if err != nil {
			return err
		}
		id, err = InsertStep(context.Background(), q, StepRow{
			TaskID: taskID, Position: pos, Title: title, Status: status,
		})
		return err
	})
	if err != nil {
		t.Fatalf("insert step %q: %v", title, err)
	}
	return id
}

// TestProjects covers EnsureProject, GetProject and UpdateProject: creation,
// the no-overwrite rule for settings, last_seen_at moving on, not-found, and
// the scoped update.
func TestProjects(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()

	if _, err := GetProject(ctx, s, "acme-api"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetProject before create: err = %v, want ErrNotFound", err)
	}

	seedProjects(t, s, "acme-api")
	later := t0.Add(time.Hour)
	// A second call must keep the settings and move last_seen_at only.
	if err := EnsureProject(ctx, s, ProjectRow{
		Name: "acme-api", CreatedAt: later, LastSeenAt: later, Settings: `{"x":1}`,
	}); err != nil {
		t.Fatalf("EnsureProject again: %v", err)
	}
	p, err := GetProject(ctx, s, "acme-api")
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if p.Settings != `{"parallel_tasks":false}` {
		t.Errorf("settings overwritten: %s", p.Settings)
	}
	if !p.CreatedAt.Equal(t0) {
		t.Errorf("created_at = %v, want %v", p.CreatedAt, t0)
	}
	if !p.LastSeenAt.Equal(later) {
		t.Errorf("last_seen_at = %v, want %v", p.LastSeenAt, later)
	}
	if p.DisplayName != "" {
		t.Errorf("display name = %q, want empty (NULL)", p.DisplayName)
	}

	cases := []struct {
		name    string
		row     ProjectRow
		wantErr error
		want    string
	}{
		{"update existing", ProjectRow{Name: "acme-api", DisplayName: "Acme API", Settings: `{"y":2}`}, nil, "Acme API"},
		{"update missing", ProjectRow{Name: "nope", Settings: `{}`}, ErrNotFound, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := s.WithTx(ctx, func(q Querier) error { return UpdateProject(ctx, q, c.row) })
			if c.wantErr == nil && err != nil {
				t.Fatalf("UpdateProject: %v", err)
			}
			if c.wantErr != nil && !errors.Is(err, c.wantErr) {
				t.Fatalf("UpdateProject err = %v, want %v", err, c.wantErr)
			}
			if c.wantErr != nil {
				return
			}
			got, err := GetProject(ctx, s, c.row.Name)
			if err != nil {
				t.Fatalf("GetProject: %v", err)
			}
			if got.DisplayName != c.want || got.Settings != c.row.Settings {
				t.Errorf("after update: name %q settings %q, want %q %q",
					got.DisplayName, got.Settings, c.want, c.row.Settings)
			}
		})
	}
}

// TestSessions covers UpsertSession and GetSession: the first write, a second
// write that moves last_seen_at only, and not-found.
func TestSessions(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api")

	if _, err := GetSession(ctx, s, "sess-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSession before create: err = %v, want ErrNotFound", err)
	}
	first := SessionRow{ID: "sess-1", Project: "acme-api", Client: "claude-code",
		Agent: "main", StartedAt: t0, LastSeenAt: t0}
	if err := s.WithTx(ctx, func(q Querier) error { return UpsertSession(ctx, q, first) }); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	later := t0.Add(time.Minute)
	second := first
	second.Client = "other"
	second.LastSeenAt = later
	if err := s.WithTx(ctx, func(q Querier) error { return UpsertSession(ctx, q, second) }); err != nil {
		t.Fatalf("UpsertSession again: %v", err)
	}
	got, err := GetSession(ctx, s, "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Client != "claude-code" {
		t.Errorf("client = %q, want the first write's value", got.Client)
	}
	if !got.LastSeenAt.Equal(later) {
		t.Errorf("last_seen_at = %v, want %v", got.LastSeenAt, later)
	}
}

// TestTaskInsertGetUpdate covers InsertTask, GetTask (scoped), GetTaskAnyProject
// and UpdateTask: nullable fields round-trip as "", a foreign project is
// not-found, and an update to a missing row is not-found.
func TestTaskInsertGetUpdate(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api", "other")
	task := newTask("task-20261028-01", "acme-api", t0)
	task.Context = "why now"
	insertTask(t, s, task)

	got, err := GetTask(ctx, s, "acme-api", "task-20261028-01")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Context != "why now" || got.Owner != "" || got.StartedAt != nil {
		t.Errorf("round trip: context %q owner %q started %v", got.Context, got.Owner, got.StartedAt)
	}
	if !got.OpenedAt.Equal(t0) || !got.UpdatedAt.Equal(t0) {
		t.Errorf("times: opened %v updated %v", got.OpenedAt, got.UpdatedAt)
	}

	if _, err := GetTask(ctx, s, "other", "task-20261028-01"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetTask from another project: err = %v, want ErrNotFound", err)
	}
	if _, err := GetTask(ctx, s, "acme-api", "task-missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetTask missing: err = %v, want ErrNotFound", err)
	}
	if _, err := GetTaskAnyProject(ctx, s, "task-20261028-01"); err != nil {
		t.Errorf("GetTaskAnyProject: %v", err)
	}
	if _, err := GetTaskAnyProject(ctx, s, "task-missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetTaskAnyProject missing: err = %v, want ErrNotFound", err)
	}

	started := t0.Add(time.Minute)
	upd := got
	upd.Status = "active"
	upd.StartedAt = &started
	upd.Owner = "main"
	upd.UpdatedAt = started
	if err := s.WithTx(ctx, func(q Querier) error { return UpdateTask(ctx, q, upd) }); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	got, err = GetTask(ctx, s, "acme-api", "task-20261028-01")
	if err != nil {
		t.Fatalf("GetTask after update: %v", err)
	}
	if got.Status != "active" || got.Owner != "main" || got.StartedAt == nil || !got.StartedAt.Equal(started) {
		t.Errorf("after update: status %q owner %q started %v", got.Status, got.Owner, got.StartedAt)
	}

	// The row carries the project it was read with. A row whose project does
	// not match the stored one matches nothing, so the write is not-found and
	// the stored row is untouched.
	wrong := upd
	wrong.Project = "other"
	wrong.Status = "done"
	err = s.WithTx(ctx, func(q Querier) error { return UpdateTask(ctx, q, wrong) })
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateTask with wrong project: err = %v, want ErrNotFound", err)
	}
	got, _ = GetTask(ctx, s, "acme-api", "task-20261028-01")
	if got.Status != "active" {
		t.Errorf("row changed by a not-found update: status %q", got.Status)
	}
}

// TestListTasks covers the status filter, keyset paging in updated_at order, the
// more flag, and the project scope.
func TestListTasks(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api", "other")
	// Five tasks in acme-api; two share an updated_at, to test the id tie-break.
	insertTask(t, s, newTask("task-20261028-01", "acme-api", t0.Add(1*time.Minute)))
	insertTask(t, s, newTask("task-20261028-02", "acme-api", t0.Add(2*time.Minute)))
	insertTask(t, s, newTask("task-20261028-03", "acme-api", t0.Add(2*time.Minute)))
	insertTask(t, s, newTask("task-20261028-04", "acme-api", t0.Add(3*time.Minute)))
	insertTask(t, s, newTask("task-20261028-05", "acme-api", t0.Add(4*time.Minute)))
	insertTask(t, s, newTask("task-20261028-06", "other", t0.Add(5*time.Minute)))

	ids := func(rows []TaskRow) string {
		var b []string
		for _, r := range rows {
			b = append(b, r.ID)
		}
		return strings.Join(b, ",")
	}

	// Newest first, ties broken by id, and the task of "other" never appears.
	const all = "task-20261028-05,task-20261028-04,task-20261028-03,task-20261028-02,task-20261028-01"
	rows, more, err := ListTasks(ctx, s, "acme-api", nil, nil, 10)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if more {
		t.Error("more = true with a limit above the count")
	}
	if got := ids(rows); got != all {
		t.Errorf("ids = %s, want %s", got, all)
	}

	// Status filter: only open tasks, and a status that matches none.
	rows, _, err = ListTasks(ctx, s, "acme-api", []string{"open"}, nil, 10)
	if err != nil || len(rows) != 5 {
		t.Fatalf("status open: %d rows, err %v; want 5", len(rows), err)
	}
	rows, _, err = ListTasks(ctx, s, "acme-api", []string{"done"}, nil, 10)
	if err != nil || len(rows) != 0 {
		t.Errorf("status done: %d rows, err %v; want none", len(rows), err)
	}

	// Paging: page size 2 walks all five with the keyset of the last row.
	var walked []string
	var after *TaskKey
	for page := 0; page < 5; page++ {
		rows, more, err := ListTasks(ctx, s, "acme-api", nil, after, 2)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		for _, r := range rows {
			walked = append(walked, r.ID)
		}
		if !more {
			break
		}
		last := rows[len(rows)-1]
		after = &TaskKey{UpdatedAt: last.UpdatedAt, ID: last.ID}
	}
	want := "task-20261028-05,task-20261028-04,task-20261028-03,task-20261028-02,task-20261028-01"
	if got := strings.Join(walked, ","); got != want {
		t.Errorf("walked pages = %s, want %s", got, want)
	}

	if _, _, err := ListTasks(ctx, s, "acme-api", nil, nil, 0); err == nil {
		t.Error("ListTasks with limit 0 succeeded")
	}
}

// TestCriteria covers ReplaceCriteria and ListCriteria: the list is replaced as
// a whole, rows come back in ord order, and an empty result is NULL.
func TestCriteria(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api")
	insertTask(t, s, newTask("task-20261028-01", "acme-api", t0))

	err := s.WithTx(ctx, func(q Querier) error {
		return ReplaceCriteria(ctx, q, "task-20261028-01", []CriterionRow{
			{TaskID: "task-20261028-01", Ord: 2, Criterion: "second"},
			{TaskID: "task-20261028-01", Ord: 1, Criterion: "first", Result: "met", Evidence: "tests"},
		})
	})
	if err != nil {
		t.Fatalf("ReplaceCriteria: %v", err)
	}
	rows, err := ListCriteria(ctx, s, "task-20261028-01")
	if err != nil {
		t.Fatalf("ListCriteria: %v", err)
	}
	if len(rows) != 2 || rows[0].Criterion != "first" || rows[0].Result != "met" ||
		rows[1].Criterion != "second" || rows[1].Result != "" {
		t.Fatalf("criteria = %+v, want first(met), second(empty)", rows)
	}

	err = s.WithTx(ctx, func(q Querier) error {
		return ReplaceCriteria(ctx, q, "task-20261028-01", []CriterionRow{
			{TaskID: "task-20261028-01", Ord: 1, Criterion: "only"},
		})
	})
	if err != nil {
		t.Fatalf("ReplaceCriteria again: %v", err)
	}
	rows, _ = ListCriteria(ctx, s, "task-20261028-01")
	if len(rows) != 1 || rows[0].Criterion != "only" {
		t.Errorf("after replace: %+v, want only one row", rows)
	}
}

// TestFiles covers ReplaceFiles and ListFiles: key and deliverable rows share
// the table and can both have ord 1, replacing one role leaves the other alone,
// an unknown role is refused, and a row of another role is refused.
func TestFiles(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))

	err := s.WithTx(ctx, func(q Querier) error {
		if err := ReplaceFiles(ctx, q, id, "key", []FileRow{
			{TaskID: id, Role: "key", Ord: 1, Path: "a.go", Note: "entry"},
		}); err != nil {
			return err
		}
		return ReplaceFiles(ctx, q, id, "deliverable", []FileRow{
			{TaskID: id, Role: "deliverable", Ord: 1, Path: "out.md"},
		})
	})
	if err != nil {
		t.Fatalf("ReplaceFiles: %v", err)
	}
	keys, _ := ListFiles(ctx, s, id, "key")
	dels, _ := ListFiles(ctx, s, id, "deliverable")
	if len(keys) != 1 || keys[0].Path != "a.go" || keys[0].Note != "entry" {
		t.Errorf("key files = %+v", keys)
	}
	if len(dels) != 1 || dels[0].Path != "out.md" || dels[0].Note != "" {
		t.Errorf("deliverables = %+v", dels)
	}

	// Replacing the key files with none clears them and leaves deliverables.
	if err := s.WithTx(ctx, func(q Querier) error {
		return ReplaceFiles(ctx, q, id, "key", nil)
	}); err != nil {
		t.Fatalf("clear key files: %v", err)
	}
	keys, _ = ListFiles(ctx, s, id, "key")
	dels, _ = ListFiles(ctx, s, id, "deliverable")
	if len(keys) != 0 || len(dels) != 1 {
		t.Errorf("after clearing keys: %d keys, %d deliverables; want 0 and 1", len(keys), len(dels))
	}

	refused := []struct {
		name string
		role string
		rows []FileRow
	}{
		{"unknown role", "other", nil},
		{"row of another role", "key", []FileRow{{TaskID: id, Role: "deliverable", Ord: 1, Path: "x"}}},
	}
	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			err := s.WithTx(ctx, func(q Querier) error {
				return ReplaceFiles(ctx, q, id, c.role, c.rows)
			})
			if err == nil {
				t.Error("ReplaceFiles succeeded")
			}
		})
	}
}

// TestLessonsAndMistakes covers ReplaceLessons, ListLessons, GetLesson,
// ReplaceMistakes, ListMistakes and GetMistake, including not-found.
func TestLessonsAndMistakes(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))

	err := s.WithTx(ctx, func(q Querier) error {
		if err := ReplaceLessons(ctx, q, id, []LessonRow{
			{TaskID: id, Ord: 1, Kind: "reuse", Text: "use WithTx", Why: "locks", Correction: "x"},
			{TaskID: id, Ord: 2, Kind: "similar-task", Text: "like PR 45"},
		}); err != nil {
			return err
		}
		return ReplaceMistakes(ctx, q, id, []MistakeRow{
			{TaskID: id, Ord: 1, Agent: "hicks", Mistake: "forgot gofmt", Impact: "low", Prevention: "run fmt"},
		})
	})
	if err != nil {
		t.Fatalf("replace lessons and mistakes: %v", err)
	}

	lessons, err := ListLessons(ctx, s, id)
	if err != nil || len(lessons) != 2 || lessons[0].Why != "locks" || lessons[1].Why != "" {
		t.Fatalf("ListLessons = %+v, err %v", lessons, err)
	}
	l, err := GetLesson(ctx, s, id, 2)
	if err != nil || l.Kind != "similar-task" {
		t.Errorf("GetLesson 2 = %+v, err %v", l, err)
	}
	if _, err := GetLesson(ctx, s, id, 9); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetLesson 9: err = %v, want ErrNotFound", err)
	}

	mistakes, err := ListMistakes(ctx, s, id)
	if err != nil || len(mistakes) != 1 || mistakes[0].Prevention != "run fmt" || mistakes[0].Correction != "" {
		t.Fatalf("ListMistakes = %+v, err %v", mistakes, err)
	}
	m, err := GetMistake(ctx, s, id, 1)
	if err != nil || m.Agent != "hicks" {
		t.Errorf("GetMistake 1 = %+v, err %v", m, err)
	}
	if _, err := GetMistake(ctx, s, id, 9); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetMistake 9: err = %v, want ErrNotFound", err)
	}

	// Replacing with empty lists clears both.
	if err := s.WithTx(ctx, func(q Querier) error {
		if err := ReplaceLessons(ctx, q, id, nil); err != nil {
			return err
		}
		return ReplaceMistakes(ctx, q, id, nil)
	}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	lessons, _ = ListLessons(ctx, s, id)
	mistakes, _ = ListMistakes(ctx, s, id)
	if len(lessons) != 0 || len(mistakes) != 0 {
		t.Errorf("after clear: %d lessons, %d mistakes", len(lessons), len(mistakes))
	}
}

// TestSteps covers the step queries: positions and display numbers, insertion
// between steps without renumbering ids, the scoped reads, UpdateStep, and the
// deletion of pending steps only.
func TestSteps(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))

	first := insertStepAt(t, s, id, "write test", "pending")
	second := insertStepAt(t, s, id, "implement", "pending")
	third := insertStepAt(t, s, id, "review", "pending")

	steps, err := ListSteps(ctx, s, id)
	if err != nil || len(steps) != 3 {
		t.Fatalf("ListSteps = %d rows, err %v", len(steps), err)
	}
	for i, st := range steps {
		if st.Number != i+1 {
			t.Errorf("step %q number = %d, want %d", st.Title, st.Number, i+1)
		}
	}

	got, err := GetStep(ctx, s, id, second)
	if err != nil || got.Title != "implement" || got.Number != 2 {
		t.Errorf("GetStep = %+v, err %v", got, err)
	}
	if _, err := GetStep(ctx, s, "task-20261028-02", second); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetStep under another task: err = %v, want ErrNotFound", err)
	}
	byNum, err := GetStepByNumber(ctx, s, id, 3)
	if err != nil || byNum.ID != third {
		t.Errorf("GetStepByNumber 3 = %+v, err %v", byNum, err)
	}
	if _, err := GetStepByNumber(ctx, s, id, 4); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetStepByNumber 4: err = %v, want ErrNotFound", err)
	}

	// Insert after step 1: the new step takes a position between 1 and 2, its
	// id is new, and the ids of the others stay where they were.
	first1, err := GetStep(ctx, s, id, first)
	if err != nil {
		t.Fatalf("GetStep first: %v", err)
	}
	var inserted int64
	err = s.WithTx(ctx, func(q Querier) error {
		pos, err := StepPositionBetween(ctx, q, id, &first1.Position)
		if err != nil {
			return err
		}
		inserted, err = InsertStep(ctx, q, StepRow{TaskID: id, Position: pos, Title: "design", Status: "pending"})
		return err
	})
	if err != nil {
		t.Fatalf("insert between: %v", err)
	}
	steps, _ = ListSteps(ctx, s, id)
	wantTitles := []string{"write test", "design", "implement", "review"}
	for i, st := range steps {
		if st.Title != wantTitles[i] || st.Number != i+1 {
			t.Errorf("after insert, position %d = %q number %d, want %q", i, st.Title, st.Number, wantTitles[i])
		}
	}
	if steps[2].ID != second || steps[1].ID != inserted {
		t.Errorf("ids moved or wrong: inserted %d, steps %+v", inserted, steps)
	}

	// Positions for the edges: before the first step, and after the last.
	err = s.WithTx(ctx, func(q Querier) error {
		before, err := StepPositionBetween(ctx, q, id, nil)
		if err != nil {
			return err
		}
		if !(before > 0 && before < first1.Position) {
			t.Errorf("before first = %v, want between 0 and %v", before, first1.Position)
		}
		last, err := StepPositionLast(ctx, q, id)
		if err != nil {
			return err
		}
		lastStep := steps[len(steps)-1]
		if last <= lastStep.Position {
			t.Errorf("last = %v, want above %v", last, lastStep.Position)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("edge positions: %v", err)
	}

	// Empty task: the first position is 1 for both ends.
	const empty = "task-20261028-02"
	insertTask(t, s, newTask(empty, "acme-api", t0))
	err = s.WithTx(ctx, func(q Querier) error {
		p, err := StepPositionLast(ctx, q, empty)
		if err != nil || p != 1 {
			t.Errorf("empty task last = %v, err %v; want 1", p, err)
		}
		p, err = StepPositionBetween(ctx, q, empty, nil)
		if err != nil || p != 1 {
			t.Errorf("empty task before = %v, err %v; want 1", p, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("empty task positions: %v", err)
	}

	// UpdateStep: a real change persists; a row of another task is not-found.
	st := steps[0]
	st.Status = "done"
	st.Summary = "tests written"
	ended := t0.Add(time.Minute)
	st.EndedAt = &ended
	if err := s.WithTx(ctx, func(q Querier) error { return UpdateStep(ctx, q, st) }); err != nil {
		t.Fatalf("UpdateStep: %v", err)
	}
	got, _ = GetStep(ctx, s, id, st.ID)
	if got.Status != "done" || got.Summary != "tests written" || got.EndedAt == nil {
		t.Errorf("after UpdateStep: %+v", got)
	}
	bad := st
	bad.TaskID = empty
	if err := s.WithTx(ctx, func(q Querier) error { return UpdateStep(ctx, q, bad) }); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateStep under another task: err = %v, want ErrNotFound", err)
	}

	// DeletePendingSteps removes the pending ones and keeps the done one.
	var removed int64
	if err := s.WithTx(ctx, func(q Querier) error {
		var err error
		removed, err = DeletePendingSteps(ctx, q, id)
		return err
	}); err != nil {
		t.Fatalf("DeletePendingSteps: %v", err)
	}
	steps, _ = ListSteps(ctx, s, id)
	if removed != 3 || len(steps) != 1 || steps[0].Status != "done" {
		t.Errorf("removed %d, left %+v; want 3 removed and the done step kept", removed, steps)
	}
}

// TestStepDeleteBlockedByEvent checks that a pending step an event points at is
// not silently removed: the foreign key refuses the delete, and the error comes
// back to the caller.
func TestStepDeleteBlockedByEvent(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))
	stepID := insertStepAt(t, s, id, "pending step", "pending")
	if err := s.WithTx(ctx, func(q Querier) error {
		_, err := InsertEvent(ctx, q, EventRow{TaskID: id, Project: "acme-api", StepID: &stepID,
			Agent: "main", Kind: "note", Text: "about it", OccurredAt: t0})
		return err
	}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	err := s.WithTx(ctx, func(q Querier) error {
		_, err := DeletePendingSteps(ctx, q, id)
		return err
	})
	if err == nil {
		t.Error("DeletePendingSteps removed a step an event points at")
	}
	if _, err := GetStep(ctx, s, id, stepID); err != nil {
		t.Errorf("step gone after refused delete: %v", err)
	}
}

// TestPendingStepsWithRecords covers the query step_plan uses: a pending step an
// event refers to and a pending step a review refers to are listed, in display
// order. A bare pending step is not, and neither is a done step with an event.
func TestPendingStepsWithRecords(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))
	byEvent := insertStepAt(t, s, id, "event step", "pending")
	byReview := insertStepAt(t, s, id, "review step", "pending")
	insertStepAt(t, s, id, "bare step", "pending")
	done := insertStepAt(t, s, id, "done step", "done")
	if err := s.WithTx(ctx, func(q Querier) error {
		if _, err := InsertEvent(ctx, q, EventRow{TaskID: id, Project: "acme-api", StepID: &byEvent,
			Agent: "main", Kind: "note", Text: "about it", OccurredAt: t0}); err != nil {
			return err
		}
		if _, err := InsertEvent(ctx, q, EventRow{TaskID: id, Project: "acme-api", StepID: &done,
			Agent: "main", Kind: "note", Text: "after done", OccurredAt: t0}); err != nil {
			return err
		}
		_, err := InsertReview(ctx, q, ReviewRow{TaskID: id, StepID: byReview, Reviewer: "reviewer",
			Verdict: "approved", CreatedAt: t0})
		return err
	}); err != nil {
		t.Fatalf("write records: %v", err)
	}

	ids, err := PendingStepsWithRecords(ctx, s, id)
	if err != nil {
		t.Fatalf("PendingStepsWithRecords: %v", err)
	}
	if len(ids) != 2 || ids[0] != byEvent || ids[1] != byReview {
		t.Errorf("PendingStepsWithRecords = %v, want [%d %d] in display order", ids, byEvent, byReview)
	}
	none, err := PendingStepsWithRecords(ctx, s, "task-20261028-99")
	if err != nil || len(none) != 0 {
		t.Errorf("PendingStepsWithRecords of an unknown task = %v, %v; want none", none, err)
	}
}

// TestEvents covers InsertEvent and ListEvents: newest first, the step filter,
// keyset paging with Before, the project scope, and the nullable columns.
func TestEvents(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api", "other")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))
	stepID := insertStepAt(t, s, id, "s", "pending")

	var ids []int64
	for i, text := range []string{"one", "two", "three"} {
		var sid *int64
		if i == 1 {
			sid = &stepID
		}
		var eid int64
		err := s.WithTx(ctx, func(q Querier) error {
			var err error
			eid, err = InsertEvent(ctx, q, EventRow{TaskID: id, Project: "acme-api", StepID: sid,
				Agent: "main", Kind: "note", Text: text, Data: `{"n":1}`, OccurredAt: t0.Add(time.Duration(i) * time.Second)})
			return err
		})
		if err != nil {
			t.Fatalf("InsertEvent %s: %v", text, err)
		}
		ids = append(ids, eid)
	}

	rows, more, err := ListEvents(ctx, s, EventFilter{TaskID: id, Project: "acme-api", Limit: 10})
	if err != nil || more || len(rows) != 3 {
		t.Fatalf("ListEvents = %d rows, more %v, err %v", len(rows), more, err)
	}
	if rows[0].Text != "three" || rows[2].Text != "one" {
		t.Errorf("order = %q..%q, want newest first", rows[0].Text, rows[2].Text)
	}
	if rows[1].StepID == nil || *rows[1].StepID != stepID || rows[0].StepID != nil {
		t.Errorf("step ids = %v and %v", rows[0].StepID, rows[1].StepID)
	}
	if rows[1].Data != `{"n":1}` || rows[0].Data != `{"n":1}` {
		t.Errorf("data = %q", rows[1].Data)
	}

	byStep, _, err := ListEvents(ctx, s, EventFilter{TaskID: id, Project: "acme-api", StepID: &stepID, Limit: 10})
	if err != nil || len(byStep) != 1 || byStep[0].Text != "two" {
		t.Errorf("step filter = %+v, err %v", byStep, err)
	}

	page1, more, err := ListEvents(ctx, s, EventFilter{TaskID: id, Project: "acme-api", Limit: 2})
	if err != nil || !more || len(page1) != 2 {
		t.Fatalf("page 1 = %d rows, more %v, err %v", len(page1), more, err)
	}
	before := page1[len(page1)-1].ID
	page2, more, err := ListEvents(ctx, s, EventFilter{TaskID: id, Project: "acme-api", Before: &before, Limit: 2})
	if err != nil || more || len(page2) != 1 || page2[0].ID != ids[0] {
		t.Errorf("page 2 = %+v, more %v, err %v", page2, more, err)
	}

	other, _, err := ListEvents(ctx, s, EventFilter{TaskID: id, Project: "other", Limit: 10})
	if err != nil || len(other) != 0 {
		t.Errorf("events read under another project = %d rows, err %v; want none", len(other), err)
	}
	if _, _, err := ListEvents(ctx, s, EventFilter{TaskID: id, Project: "acme-api"}); err == nil {
		t.Error("ListEvents with limit 0 succeeded")
	}
}

// TestReviews covers InsertReview and ListReviews: the verdict and counts round
// trip, and the reviews of one task come back in the order they were made.
func TestReviews(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))
	stepID := insertStepAt(t, s, id, "s", "pending")

	for i, verdict := range []string{"not-approved", "approved"} {
		err := s.WithTx(ctx, func(q Querier) error {
			_, err := InsertReview(ctx, q, ReviewRow{TaskID: id, StepID: stepID, Reviewer: "apone",
				Verdict: verdict, Critical: i, Warnings: 2, Summary: "round", CreatedAt: t0.Add(time.Duration(i) * time.Second)})
			return err
		})
		if err != nil {
			t.Fatalf("InsertReview %d: %v", i, err)
		}
	}
	reviews, err := ListReviews(ctx, s, id)
	if err != nil || len(reviews) != 2 {
		t.Fatalf("ListReviews = %d, err %v", len(reviews), err)
	}
	if reviews[0].Verdict != "not-approved" || reviews[1].Verdict != "approved" || reviews[1].Critical != 1 {
		t.Errorf("reviews = %+v", reviews)
	}
	if reviews[0].Reviewer != "apone" || reviews[0].Warnings != 2 || reviews[0].Summary != "round" {
		t.Errorf("fields = %+v", reviews[0])
	}
}

// TestLearnings covers InsertLearning, the scoped and unscoped gets, UpdateLearning
// and ListLearnings with its status, kind, target, project and paging filters.
func TestLearnings(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api", "other")

	insert := func(project, kind, status, target string, at time.Time) int64 {
		t.Helper()
		var id int64
		err := s.WithTx(ctx, func(q Querier) error {
			var err error
			id, err = InsertLearning(ctx, q, LearningRow{Project: project, Agent: "main",
				Kind: kind, Title: "t", Body: "b", Target: target, Status: status, CreatedAt: at})
			return err
		})
		if err != nil {
			t.Fatalf("InsertLearning: %v", err)
		}
		return id
	}
	a := insert("acme-api", "finding", "proposed", "agents/hicks.md", t0)
	b := insert("acme-api", "pattern", "accepted", "", t0.Add(time.Second))
	c := insert("acme-api", "finding", "proposed", "agents/hicks.md", t0.Add(2*time.Second))
	o := insert("other", "finding", "proposed", "", t0.Add(3*time.Second))

	got, err := GetLearning(ctx, s, "acme-api", a)
	if err != nil || got.Target != "agents/hicks.md" || got.TaskID != "" || got.DecidedAt != nil {
		t.Errorf("GetLearning = %+v, err %v", got, err)
	}
	if _, err := GetLearning(ctx, s, "acme-api", o); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetLearning across projects: err = %v, want ErrNotFound", err)
	}
	if _, err := GetLearningAnyProject(ctx, s, o); err != nil {
		t.Errorf("GetLearningAnyProject: %v", err)
	}
	if _, err := GetLearningAnyProject(ctx, s, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetLearningAnyProject missing: err = %v, want ErrNotFound", err)
	}

	// Decide: accepted, decided by the operator, with a note.
	decided := got
	decided.Status = "rejected"
	decidedAt := t0.Add(time.Hour)
	decided.DecidedBy = "operator"
	decided.DecidedAt = &decidedAt
	decided.DecisionNote = "not now"
	if err := s.WithTx(ctx, func(q Querier) error { return UpdateLearning(ctx, q, decided) }); err != nil {
		t.Fatalf("UpdateLearning: %v", err)
	}
	got, _ = GetLearning(ctx, s, "acme-api", a)
	if got.Status != "rejected" || got.DecidedBy != "operator" || got.DecidedAt == nil || got.DecisionNote != "not now" {
		t.Errorf("after decision: %+v", got)
	}
	missing := decided
	missing.ID = 999
	if err := s.WithTx(ctx, func(q Querier) error { return UpdateLearning(ctx, q, missing) }); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateLearning missing: err = %v, want ErrNotFound", err)
	}

	ids := func(rows []LearningRow) []int64 {
		var out []int64
		for _, r := range rows {
			out = append(out, r.ID)
		}
		return out
	}
	cases := []struct {
		name   string
		filter LearningFilter
		want   []int64
	}{
		{"project scope, newest first", LearningFilter{Project: "acme-api", Limit: 10}, []int64{c, b, a}},
		{"all projects", LearningFilter{AllProjects: true, Limit: 10}, []int64{o, c, b, a}},
		{"status proposed", LearningFilter{Project: "acme-api", Statuses: []string{"proposed"}, Limit: 10}, []int64{c}},
		{"kind pattern", LearningFilter{Project: "acme-api", Kinds: []string{"pattern"}, Limit: 10}, []int64{b}},
		{"target exact", LearningFilter{Project: "acme-api", Target: "agents/hicks.md", Limit: 10}, []int64{c, a}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, more, err := ListLearnings(ctx, s, tc.filter)
			if err != nil || more {
				t.Fatalf("ListLearnings err %v, more %v", err, more)
			}
			if !sameIDs(ids(rows), tc.want) {
				t.Errorf("ids = %v, want %v", ids(rows), tc.want)
			}
		})
	}

	page, more, err := ListLearnings(ctx, s, LearningFilter{AllProjects: true, Limit: 2})
	if err != nil || !more || len(page) != 2 {
		t.Fatalf("page 1 = %d, more %v, err %v", len(page), more, err)
	}
	before := page[len(page)-1].ID
	next, more, err := ListLearnings(ctx, s, LearningFilter{AllProjects: true, Before: &before, Limit: 2})
	if err != nil || more || !sameIDs(ids(next), []int64{b, a}) {
		t.Errorf("page 2 = %v, more %v, err %v", ids(next), more, err)
	}
	if _, _, err := ListLearnings(ctx, s, LearningFilter{Project: "acme-api"}); err == nil {
		t.Error("ListLearnings with limit 0 succeeded")
	}
}

// sameIDs reports whether two id lists hold the same values in the same order.
func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestDirectives covers NextDirectiveCode, InsertDirective, GetDirectiveByCode,
// ListActiveDirectives with its scopes and retired rows, and UpdateDirectiveStatus.
func TestDirectives(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api", "other")

	code, err := NextDirectiveCode(ctx, s)
	if err != nil || code != "D-0001" {
		t.Fatalf("first code = %q, err %v; want D-0001", code, err)
	}
	add := func(scope, project, agent, title string) string {
		t.Helper()
		var c string
		err := s.WithTx(ctx, func(q Querier) error {
			var err error
			c, err = NextDirectiveCode(ctx, q)
			if err != nil {
				return err
			}
			_, err = InsertDirective(ctx, q, DirectiveRow{Code: c, Scope: scope, Project: project,
				Agent: agent, Title: title, Rule: "rule " + title, Status: "active",
				RatifiedBy: "operator", RatifiedAt: &t0})
			return err
		})
		if err != nil {
			t.Fatalf("insert directive %s: %v", title, err)
		}
		return c
	}
	global := add("global", "", "", "global")
	acmeOnly := add("project", "acme-api", "", "acme")
	otherOnly := add("project", "other", "", "other")
	hicks := add("agent", "", "hicks", "hicks")
	reviewer := add("agent", "", "reviewer", "reviewer")
	retired := add("global", "", "", "retired")
	if err := s.WithTx(ctx, func(q Querier) error {
		return UpdateDirectiveStatus(ctx, q, retired, "retired", &t0)
	}); err != nil {
		t.Fatalf("UpdateDirectiveStatus: %v", err)
	}

	next, err := NextDirectiveCode(ctx, s)
	if err != nil || next != "D-0007" {
		t.Errorf("next after six = %q, err %v; want D-0007 (retired codes never reused)", next, err)
	}

	d, err := GetDirectiveByCode(ctx, s, hicks)
	if err != nil || d.Agent != "hicks" || d.Scope != "agent" || d.Project != "" {
		t.Errorf("GetDirectiveByCode = %+v, err %v", d, err)
	}
	if _, err := GetDirectiveByCode(ctx, s, "D-9999"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDirectiveByCode missing: err = %v, want ErrNotFound", err)
	}
	r, _ := GetDirectiveByCode(ctx, s, retired)
	if r.Status != "retired" || r.RetiredAt == nil {
		t.Errorf("retired row = %+v", r)
	}
	if err := s.WithTx(ctx, func(q Querier) error {
		return UpdateDirectiveStatus(ctx, q, "D-9999", "retired", &t0)
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateDirectiveStatus missing: err = %v, want ErrNotFound", err)
	}

	codesOf := func(rows []DirectiveRow) string {
		var out []string
		for _, r := range rows {
			out = append(out, r.Code)
		}
		return strings.Join(out, ",")
	}
	cases := []struct {
		name, project, agent string
		allAgents            bool
		want                 string
	}{
		{"acme, every agent: global, its project, every agent", "acme-api", "", true,
			strings.Join([]string{global, acmeOnly, hicks, reviewer}, ",")},
		{"acme, hicks: only hicks among agents", "acme-api", "hicks", false,
			strings.Join([]string{global, acmeOnly, hicks}, ",")},
		{"acme, no role: an empty agent is no agent, not every agent (12-W1)", "acme-api", "", false,
			strings.Join([]string{global, acmeOnly}, ",")},
		{"other project does not see acme's project directive", "other", "", true,
			strings.Join([]string{global, otherOnly, hicks, reviewer}, ",")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, err := ListActiveDirectives(ctx, s, c.project, c.agent, c.allAgents)
			if err != nil {
				t.Fatalf("ListActiveDirectives: %v", err)
			}
			if got := codesOf(rows); got != c.want {
				t.Errorf("codes = %s, want %s", got, c.want)
			}
		})
	}
}

// TestNotes covers InsertNote, GetNote (scoped), UpdateNote, and the NULL task
// for a note with no task.
func TestNotes(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	seedProjects(t, s, "acme-api", "other")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))

	var attached, loose int64
	err := s.WithTx(ctx, func(q Querier) error {
		var err error
		attached, err = InsertNote(ctx, q, NoteRow{TaskID: id, Project: "acme-api", Title: "design",
			Body: "# body", CreatedBy: "main", UpdatedAt: t0})
		if err != nil {
			return err
		}
		loose, err = InsertNote(ctx, q, NoteRow{Project: "acme-api", Title: "loose", Body: "b",
			CreatedBy: "main", UpdatedAt: t0})
		return err
	})
	if err != nil {
		t.Fatalf("InsertNote: %v", err)
	}

	n, err := GetNote(ctx, s, "acme-api", attached)
	if err != nil || n.TaskID != id || n.Body != "# body" {
		t.Errorf("GetNote = %+v, err %v", n, err)
	}
	l, _ := GetNote(ctx, s, "acme-api", loose)
	if l.TaskID != "" {
		t.Errorf("loose note task = %q, want empty", l.TaskID)
	}
	if _, err := GetNote(ctx, s, "other", attached); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetNote across projects: err = %v, want ErrNotFound", err)
	}

	later := t0.Add(time.Minute)
	n.Title = "design v2"
	n.Body = "new body"
	n.UpdatedAt = later
	if err := s.WithTx(ctx, func(q Querier) error { return UpdateNote(ctx, q, n) }); err != nil {
		t.Fatalf("UpdateNote: %v", err)
	}
	n, _ = GetNote(ctx, s, "acme-api", attached)
	if n.Title != "design v2" || n.Body != "new body" || !n.UpdatedAt.Equal(later) || n.TaskID != id {
		t.Errorf("after update: %+v", n)
	}
	wrong := n
	wrong.Project = "other"
	if err := s.WithTx(ctx, func(q Querier) error { return UpdateNote(ctx, q, wrong) }); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateNote in another project: err = %v, want ErrNotFound", err)
	}
}

// TestParseChildRef covers the lesson and mistake ref format used by search hits.
func TestParseChildRef(t *testing.T) {
	cases := []struct {
		ref    string
		task   string
		ord    int
		failed bool
	}{
		{"task-20261028-01/3", "task-20261028-01", 3, false},
		{"task-20261028-01", "", 0, true},
		{"task-20261028-01/x", "", 0, true},
		{"task-20261028-01/3x", "", 0, true},
	}
	for _, c := range cases {
		task, ord, err := ParseChildRef(c.ref)
		if c.failed {
			if err == nil {
				t.Errorf("ParseChildRef(%q) succeeded", c.ref)
			}
			continue
		}
		if err != nil || task != c.task || ord != c.ord {
			t.Errorf("ParseChildRef(%q) = %q, %d, %v", c.ref, task, ord, err)
		}
	}
}
