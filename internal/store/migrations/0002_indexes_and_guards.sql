-- Schema v2: secondary indexes for the typed queries in internal/store, and the
-- guard that keeps a task's project fixed. Applied once, in its own
-- transaction, by internal/store. Released migrations are never edited; a
-- change is a new numbered file.

-- Indexes. SQLite does not index the child side of a foreign key, so a delete
-- of a parent scans the child table without one. Each index below is named by
-- the query or the foreign key it serves.

-- steps: the step list, in display order (ROW_NUMBER over position, id).
CREATE INDEX idx_steps_task ON steps (task_id, position, id);

-- events: the journal of one task, newest first (keyset on id). The project
-- index serves the scoped journal read, which filters on project as well.
CREATE INDEX idx_events_task ON events (task_id, id);
CREATE INDEX idx_events_project ON events (project, id);
-- events.step_id: event_list filtered by step; also the foreign-key child for
-- a step delete (step_plan removes pending steps).
CREATE INDEX idx_events_step ON events (step_id);

-- reviews: the reviews of one task (task_get, steps section), and the
-- foreign-key child of steps.
CREATE INDEX idx_reviews_task ON reviews (task_id, id);
CREATE INDEX idx_reviews_step ON reviews (step_id);

-- learnings: learning_list, scoped to a project and filtered by status.
CREATE INDEX idx_learnings_project_status ON learnings (project, status, id);

-- notes: the foreign-key child of tasks. No current call lists notes by task.
CREATE INDEX idx_notes_task ON notes (task_id);

-- sessions: the foreign-key child of projects, for a per-project lookup. No
-- current call lists sessions yet.
CREATE INDEX idx_sessions_project ON sessions (project);

-- tasks: task_list, scoped to a project, most recently updated first (keyset
-- on updated_at, id).
CREATE INDEX idx_tasks_project_updated ON tasks (project, updated_at, id);

-- Guard. No CONTRACT call changes a task's project (it is set by task_start and
-- never written again). The search_index rows for a task's events, lessons and
-- mistakes copy the project, so a change would leave them stale. The WHEN
-- clause lets an update that writes the same project through.
CREATE TRIGGER tasks_project_immutable BEFORE UPDATE OF project ON tasks
WHEN NEW.project IS NOT OLD.project
BEGIN
    SELECT RAISE(ABORT, 'tasks.project is immutable');
END;
