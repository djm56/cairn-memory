package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// countTasks returns how many task rows exist with the given id.
func countTasks(t *testing.T, s *Store, id string) int {
	t.Helper()
	var n int
	if err := s.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM tasks WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("count tasks %s: %v", id, err)
	}
	return n
}

// TestWithTxCommitsOnNil checks that a write inside WithTx is kept when fn returns
// nil, and visible to a read on the Store afterwards.
func TestWithTxCommitsOnNil(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api")
	const id = "task-20261028-01"
	err := s.WithTx(context.Background(), func(q Querier) error {
		return InsertTask(context.Background(), q, newTask(id, "acme-api", t0))
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	if n := countTasks(t, s, id); n != 1 {
		t.Errorf("task rows after commit = %d, want 1", n)
	}
}

// TestWithTxRollsBackOnError checks that a write made before fn returns an error
// is discarded, and that fn's error comes back unwrapped, so errors.Is still
// matches it.
func TestWithTxRollsBackOnError(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api")
	const id = "task-20261028-01"
	sentinel := errors.New("core says no")
	err := s.WithTx(context.Background(), func(q Querier) error {
		if err := InsertTask(context.Background(), q, newTask(id, "acme-api", t0)); err != nil {
			return err
		}
		return sentinel
	})
	if err != sentinel {
		t.Errorf("WithTx error = %v, want the sentinel unwrapped", err)
	}
	if n := countTasks(t, s, id); n != 0 {
		t.Errorf("task rows after rollback = %d, want 0", n)
	}
}

// TestWithTxReleasesLockOnPanic checks that a panic inside fn rolls the
// transaction back and releases the write lock, so the next write does not wait
// out the busy timeout. The panic is re-raised to the caller.
func TestWithTxReleasesLockOnPanic(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api")
	const id = "task-20261028-01"
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was not re-raised")
			}
		}()
		s.WithTx(context.Background(), func(q Querier) error {
			if err := InsertTask(context.Background(), q, newTask(id, "acme-api", t0)); err != nil {
				return err
			}
			panic("boom")
		})
	}()
	if n := countTasks(t, s, id); n != 0 {
		t.Errorf("task rows after panic = %d, want 0", n)
	}
	// A second write proves the lock is free.
	done := make(chan error, 1)
	go func() {
		done <- s.WithTx(context.Background(), func(q Querier) error {
			return InsertTask(context.Background(), q, newTask("task-20261028-02", "acme-api", t0))
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("write after panic: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("write after panic blocked: the write lock was not released")
	}
}

// TestAllocateTaskIDSequence checks that sequential allocations, each followed by
// its insert in one transaction, give distinct ids in order, and that a new day
// starts again at 01. The concurrent case is covered by the core tests.
func TestAllocateTaskIDSequence(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api", "other")
	ctx := context.Background()

	day := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		project string
		day     time.Time
		want    string
	}{
		{"first of the day", "acme-api", day, "task-20261028-01"},
		{"second, other project, same day", "other", day, "task-20261028-02"},
		{"third, same day", "acme-api", day, "task-20261028-03"},
		{"next day restarts", "acme-api", day.AddDate(0, 0, 1), "task-20261029-01"},
	}
	var got []string
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var id string
			err := s.WithTx(ctx, func(q Querier) error {
				var err error
				id, err = AllocateTaskID(ctx, q, c.day)
				if err != nil {
					return err
				}
				return InsertTask(ctx, q, newTask(id, c.project, c.day))
			})
			if err != nil {
				t.Fatalf("allocate and insert: %v", err)
			}
			if id != c.want {
				t.Errorf("id = %s, want %s", id, c.want)
			}
			got = append(got, id)
		})
	}
	for i := 1; i < len(got); i++ {
		if got[i] == got[i-1] {
			t.Errorf("ids not distinct: %s twice", got[i])
		}
	}
}

// TestAllocateTaskIDPastNinetyNine checks that the counter goes on past 99,
// because the number is read as an integer and not compared as text.
func TestAllocateTaskIDPastNinetyNine(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api")
	ctx := context.Background()
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	if err := s.WithTx(ctx, func(q Querier) error {
		for n := 1; n <= 99; n++ {
			id := fmt.Sprintf("task-20261028-%02d", n)
			if err := InsertTask(ctx, q, newTask(id, "acme-api", day)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed 99 tasks: %v", err)
	}
	var id string
	if err := s.WithTx(ctx, func(q Querier) error {
		var err error
		id, err = AllocateTaskID(ctx, q, day)
		return err
	}); err != nil {
		t.Fatalf("AllocateTaskID: %v", err)
	}
	if id != "task-20261028-100" {
		t.Errorf("id after 99 = %s, want task-20261028-100", id)
	}
}

// TestTasksProjectIsImmutable checks the migration 0002 trigger: a change to
// tasks.project is refused by the schema, and a write that keeps the same
// project goes through.
func TestTasksProjectIsImmutable(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api", "other")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))

	_, err := s.ExecContext(context.Background(),
		`UPDATE tasks SET project = 'other' WHERE id = ?`, id)
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("changing tasks.project: err = %v, want the immutable refusal", err)
	}
	var project string
	if err := s.QueryRowContext(context.Background(),
		`SELECT project FROM tasks WHERE id = ?`, id).Scan(&project); err != nil {
		t.Fatalf("read project: %v", err)
	}
	if project != "acme-api" {
		t.Errorf("project = %q after refused change, want acme-api", project)
	}

	if _, err := s.ExecContext(context.Background(),
		`UPDATE tasks SET project = 'acme-api', title = 'renamed' WHERE id = ?`, id); err != nil {
		t.Errorf("update that keeps the project: %v", err)
	}
}

// TestTasksProjectGuardKeepsSearchRows checks why the guard exists: a task's
// events, lessons and mistakes are indexed with the task's project, so after a
// refused project change the index still names the right project.
func TestTasksProjectGuardKeepsSearchRows(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api", "other")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))
	if _, err := s.ExecContext(context.Background(), `INSERT INTO events
		(task_id, project, agent, kind, text, occurred_at)
		VALUES (?, 'acme-api', 'main', 'note', 'guarded words', 'now')`, id); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	if _, err := s.ExecContext(context.Background(),
		`UPDATE tasks SET project = 'other' WHERE id = ?`, id); err == nil {
		t.Fatal("project change was not refused")
	}
	var project string
	if err := s.QueryRowContext(context.Background(), `SELECT project FROM search_index
		WHERE kind = 'event' AND task_id = ?`, id).Scan(&project); err != nil {
		t.Fatalf("read event search row: %v", err)
	}
	if project != "acme-api" {
		t.Errorf("event search row project = %q, want acme-api", project)
	}
}
