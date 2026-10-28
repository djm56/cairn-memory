package store

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// seedTask creates the project and one task that the source rows in the
// table-driven test belong to. No searchable word appears in these rows.
func seedTask(t *testing.T, s *Store) {
	t.Helper()
	stmts := []string{
		`INSERT INTO projects (name, created_at, last_seen_at) VALUES ('acme-api', 'now', 'now')`,
		`INSERT INTO tasks (id, project, title, goal, status, priority, opened_at, updated_at)
		 VALUES ('task-20261028-01', 'acme-api', 'Seed task', 'Seed goal', 'active', 'normal', 'now', 'now')`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// hits returns the search_index rows matching word, as "kind:ref" strings.
func hits(t *testing.T, db *sql.DB, word string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT kind, ref FROM search_index WHERE search_index MATCH ? ORDER BY kind, ref`, word)
	if err != nil {
		t.Fatalf("MATCH %q: %v", word, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var kind, ref string
		if err := rows.Scan(&kind, &ref); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, kind+":"+ref)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// TestFTSTaskInsertUpdateDelete checks the tasks triggers: an inserted task is
// found by title, goal and outcome; an update replaces the old words; a delete
// removes the row.
func TestFTSTaskInsertUpdateDelete(t *testing.T) {
	s := openTemp(t)
	seedTask(t, s)

	// The seed task is indexed on insert. Its own words are the baseline.
	if got := hits(t, s.db, "seed"); len(got) != 1 || got[0] != "task:task-20261028-01" {
		t.Fatalf("seed task hits = %v, want one task row", got)
	}

	if _, err := s.db.Exec(`UPDATE tasks SET goal = 'throttle requests', outcome_summary = 'shipped'
		WHERE id = 'task-20261028-01'`); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := hits(t, s.db, "seed"); len(got) != 1 {
		t.Errorf("title word lost on update: hits = %v", got)
	}
	if got := hits(t, s.db, "throttle"); len(got) != 1 {
		t.Errorf("updated goal not indexed: hits = %v", got)
	}
	if got := hits(t, s.db, "shipped"); len(got) != 1 {
		t.Errorf("outcome not indexed: hits = %v", got)
	}

	if _, err := s.db.Exec(`DELETE FROM tasks WHERE id = 'task-20261028-01'`); err != nil {
		t.Fatalf("delete: %v", err)
	}
	for _, w := range []string{"seed", "throttle", "shipped"} {
		if got := hits(t, s.db, w); len(got) != 0 {
			t.Errorf("deleted task still found by %q: %v", w, got)
		}
	}
}

// TestFTSSourcesInsertUpdateDelete runs the same three steps for every other
// source table: insert a row with one word, update it to a second word, delete
// it. Each step must move the index with it. Each statement takes the word as
// its one %s.
func TestFTSSourcesInsertUpdateDelete(t *testing.T) {
	cases := []struct {
		name, kind             string
		insert, update, remove string
		// wantProject and wantTaskID are checked after the insert for the
		// kinds that carry them: a lesson, a mistake and an event.
		wantProject, wantTaskID string
	}{
		{
			name: "event",
			kind: "event",
			insert: `INSERT INTO events (id, task_id, project, agent, kind, text, occurred_at)
			         VALUES (1, 'task-20261028-01', 'acme-api', 'main', 'note', '%s', 'now')`,
			update:      `UPDATE events SET text = '%s' WHERE id = 1`,
			remove:      `DELETE FROM events WHERE id = 1`,
			wantProject: "acme-api",
			wantTaskID:  "task-20261028-01",
		},
		{
			name: "learning",
			kind: "learning",
			insert: `INSERT INTO learnings (id, project, agent, kind, title, body, status, created_at)
			         VALUES (1, 'acme-api', 'main', 'finding', 'title', '%s', 'proposed', 'now')`,
			update: `UPDATE learnings SET body = '%s' WHERE id = 1`,
			remove: `DELETE FROM learnings WHERE id = 1`,
		},
		{
			name: "directive",
			kind: "directive",
			insert: `INSERT INTO directives (id, code, scope, title, rule, status)
			         VALUES (1, 'D-0001', 'global', 'title', '%s', 'active')`,
			update: `UPDATE directives SET rule = '%s' WHERE id = 1`,
			remove: `DELETE FROM directives WHERE id = 1`,
		},
		{
			name: "note",
			kind: "note",
			insert: `INSERT INTO notes (id, project, title, body, created_by, updated_at)
			         VALUES (1, 'acme-api', 'title', '%s', 'main', 'now')`,
			update: `UPDATE notes SET body = '%s' WHERE id = 1`,
			remove: `DELETE FROM notes WHERE id = 1`,
		},
		{
			name: "lesson",
			kind: "lesson",
			insert: `INSERT INTO task_lessons (task_id, ord, kind, text)
			         VALUES ('task-20261028-01', 1, 'reuse', '%s')`,
			update:      `UPDATE task_lessons SET text = '%s' WHERE task_id = 'task-20261028-01' AND ord = 1`,
			remove:      `DELETE FROM task_lessons WHERE task_id = 'task-20261028-01' AND ord = 1`,
			wantProject: "acme-api",
			wantTaskID:  "task-20261028-01",
		},
		{
			name: "mistake",
			kind: "mistake",
			insert: `INSERT INTO task_mistakes (task_id, ord, agent, mistake, impact)
			         VALUES ('task-20261028-01', 1, 'main', '%s', 'low')`,
			update:      `UPDATE task_mistakes SET mistake = '%s' WHERE task_id = 'task-20261028-01' AND ord = 1`,
			remove:      `DELETE FROM task_mistakes WHERE task_id = 'task-20261028-01' AND ord = 1`,
			wantProject: "acme-api",
			wantTaskID:  "task-20261028-01",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := openTemp(t)
			seedTask(t, s)

			if _, err := s.db.Exec(fmt.Sprintf(c.insert, "alpha")); err != nil {
				t.Fatalf("insert: %v", err)
			}
			if got := hits(t, s.db, "alpha"); len(got) != 1 || !strings.HasPrefix(got[0], c.kind+":") {
				t.Fatalf("after insert, alpha hits = %v, want one %s", got, c.kind)
			}
			if c.wantProject != "" {
				var project, taskID string
				// Only one row of this kind exists at this point (seedTask adds
				// tasks, not these kinds), so the kind alone identifies it.
				if err := s.db.QueryRow(`SELECT project, task_id FROM search_index
					WHERE kind = ?`, c.kind).Scan(&project, &taskID); err != nil {
					t.Fatalf("read %s row: %v", c.kind, err)
				}
				if project != c.wantProject || taskID != c.wantTaskID {
					t.Errorf("%s row project %q task_id %q, want %q %q",
						c.kind, project, taskID, c.wantProject, c.wantTaskID)
				}
			}

			if _, err := s.db.Exec(fmt.Sprintf(c.update, "bravo")); err != nil {
				t.Fatalf("update: %v", err)
			}
			if got := hits(t, s.db, "alpha"); len(got) != 0 {
				t.Errorf("after update, old word still indexed: %v", got)
			}
			if got := hits(t, s.db, "bravo"); len(got) != 1 {
				t.Errorf("after update, new word hits = %v, want one", got)
			}

			if _, err := s.db.Exec(c.remove); err != nil {
				t.Fatalf("delete: %v", err)
			}
			if got := hits(t, s.db, "bravo"); len(got) != 0 {
				t.Errorf("after delete, row still indexed: %v", got)
			}
		})
	}
}
