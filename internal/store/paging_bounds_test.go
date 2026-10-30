package store

import (
	"context"
	"testing"
	"time"
)

// These tests pin the paging bounds of the four store list functions that take
// a limit: ListEvents, ListLearnings, Search and ListTasks. Each reads one row
// more than the limit to learn whether more follow. An exact-fit page (exactly
// limit rows in the store) must report no more rows, and a limit of one must be
// accepted and must page.

// insertEventRows writes n events to task id of acme-api, one second apart with
// the text given, and returns their ids, oldest first.
func insertEventRows(t *testing.T, s *Store, id, text string, n int) []int64 {
	t.Helper()
	ctx := context.Background()
	var ids []int64
	for i := 0; i < n; i++ {
		var eid int64
		err := s.WithTx(ctx, func(q Querier) error {
			var err error
			eid, err = InsertEvent(ctx, q, EventRow{TaskID: id, Project: "acme-api", Agent: "main",
				Kind: "note", Text: text, OccurredAt: t0.Add(time.Duration(i) * time.Second)})
			return err
		})
		if err != nil {
			t.Fatalf("InsertEvent %d: %v", i, err)
		}
		ids = append(ids, eid)
	}
	return ids
}

// insertLearningRows writes n proposed learnings to acme-api, one second apart,
// and returns their ids, oldest first.
func insertLearningRows(t *testing.T, s *Store, n int) []int64 {
	t.Helper()
	ctx := context.Background()
	var ids []int64
	for i := 0; i < n; i++ {
		var id int64
		err := s.WithTx(ctx, func(q Querier) error {
			var err error
			id, err = InsertLearning(ctx, q, LearningRow{Project: "acme-api", Agent: "main",
				Kind: "finding", Title: "t", Body: "b", Status: "proposed",
				CreatedAt: t0.Add(time.Duration(i) * time.Second)})
			return err
		})
		if err != nil {
			t.Fatalf("InsertLearning %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	return ids
}

// TestListEventsExactFit checks ListEvents with exactly Limit events: all three
// come back and more is false.
func TestListEventsExactFit(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))
	insertEventRows(t, s, id, "event", 3)

	rows, more, err := ListEvents(context.Background(), s, EventFilter{TaskID: id, Project: "acme-api", Limit: 3})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if more || len(rows) != 3 {
		t.Errorf("exact fit = %d rows, more %v; want 3 rows and no more", len(rows), more)
	}
}

// TestListEventsLimitOne checks ListEvents with a limit of one over two events:
// the newest comes back with more set, and the page after it holds the oldest.
func TestListEventsLimitOne(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))
	ids := insertEventRows(t, s, id, "event", 2)

	rows, more, err := ListEvents(context.Background(), s, EventFilter{TaskID: id, Project: "acme-api", Limit: 1})
	if err != nil || !more || len(rows) != 1 || rows[0].ID != ids[1] {
		t.Fatalf("limit 1 = %d rows, more %v, err %v; want the newest and more", len(rows), more, err)
	}
	before := rows[0].ID
	next, more, err := ListEvents(context.Background(), s, EventFilter{TaskID: id, Project: "acme-api", Before: &before, Limit: 1})
	if err != nil || more || len(next) != 1 || next[0].ID != ids[0] {
		t.Errorf("limit 1, page 2 = %d rows, more %v, err %v; want the oldest and no more", len(next), more, err)
	}
}

// TestListLearningsExactFit checks ListLearnings with exactly Limit learnings:
// all three come back and more is false.
func TestListLearningsExactFit(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api")
	insertLearningRows(t, s, 3)

	rows, more, err := ListLearnings(context.Background(), s, LearningFilter{Project: "acme-api", Limit: 3})
	if err != nil {
		t.Fatalf("ListLearnings: %v", err)
	}
	if more || len(rows) != 3 {
		t.Errorf("exact fit = %d rows, more %v; want 3 rows and no more", len(rows), more)
	}
}

// TestListLearningsLimitOne checks ListLearnings with a limit of one over two
// learnings: the newest comes back with more set, and the page after it holds
// the oldest.
func TestListLearningsLimitOne(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api")
	ids := insertLearningRows(t, s, 2)

	rows, more, err := ListLearnings(context.Background(), s, LearningFilter{Project: "acme-api", Limit: 1})
	if err != nil || !more || len(rows) != 1 || rows[0].ID != ids[1] {
		t.Fatalf("limit 1 = %d rows, more %v, err %v; want the newest and more", len(rows), more, err)
	}
	before := rows[0].ID
	next, more, err := ListLearnings(context.Background(), s, LearningFilter{Project: "acme-api", Before: &before, Limit: 1})
	if err != nil || more || len(next) != 1 || next[0].ID != ids[0] {
		t.Errorf("limit 1, page 2 = %d rows, more %v, err %v; want the oldest and no more", len(next), more, err)
	}
}

// TestSearchExactFit checks Search with exactly Limit hits: all three come back
// and more is false. The three events are the only rows that hold "zebra".
func TestSearchExactFit(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))
	insertEventRows(t, s, id, "zebra crossing", 3)

	rows, more, err := Search(context.Background(), s, SearchFilter{Query: "zebra", Project: "acme-api", Limit: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if more || len(rows) != 3 {
		t.Errorf("exact fit = %d hits, more %v; want 3 hits and no more", len(rows), more)
	}
}

// TestSearchLimitOne checks Search with a limit of one over two hits: the first
// comes back with more set, and the page at offset one holds the second, with
// no more.
func TestSearchLimitOne(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api")
	const id = "task-20261028-01"
	insertTask(t, s, newTask(id, "acme-api", t0))
	insertEventRows(t, s, id, "zebra crossing", 2)

	rows, more, err := Search(context.Background(), s, SearchFilter{Query: "zebra", Project: "acme-api", Limit: 1})
	if err != nil || !more || len(rows) != 1 {
		t.Fatalf("limit 1 = %d hits, more %v, err %v; want one hit and more", len(rows), more, err)
	}
	next, more, err := Search(context.Background(), s, SearchFilter{Query: "zebra", Project: "acme-api", Offset: 1, Limit: 1})
	if err != nil || more || len(next) != 1 {
		t.Errorf("limit 1, offset 1 = %d hits, more %v, err %v; want one hit and no more", len(next), more, err)
	}
}

// TestListTasksExactFit checks ListTasks with exactly limit tasks: all three
// come back and more is false.
func TestListTasksExactFit(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api")
	for i, id := range []string{"task-20261028-01", "task-20261028-02", "task-20261028-03"} {
		insertTask(t, s, newTask(id, "acme-api", t0.Add(time.Duration(i)*time.Minute)))
	}

	rows, more, err := ListTasks(context.Background(), s, "acme-api", nil, nil, 3)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if more || len(rows) != 3 {
		t.Errorf("exact fit = %d rows, more %v; want 3 rows and no more", len(rows), more)
	}
}

// TestListTasksLimitOne checks ListTasks with a limit of one over two tasks: the
// newer comes back with more set, and the page after its key holds the older.
func TestListTasksLimitOne(t *testing.T) {
	s := openTemp(t)
	seedProjects(t, s, "acme-api")
	insertTask(t, s, newTask("task-20261028-01", "acme-api", t0))
	insertTask(t, s, newTask("task-20261028-02", "acme-api", t0.Add(time.Minute)))

	rows, more, err := ListTasks(context.Background(), s, "acme-api", nil, nil, 1)
	if err != nil || !more || len(rows) != 1 || rows[0].ID != "task-20261028-02" {
		t.Fatalf("limit 1 = %d rows, more %v, err %v; want the newer task and more", len(rows), more, err)
	}
	last := rows[0]
	next, more, err := ListTasks(context.Background(), s, "acme-api", nil,
		&TaskKey{UpdatedAt: last.UpdatedAt, ID: last.ID}, 1)
	if err != nil || more || len(next) != 1 || next[0].ID != "task-20261028-01" {
		t.Errorf("limit 1, page 2 = %d rows, more %v, err %v; want the older task and no more", len(next), more, err)
	}
}
