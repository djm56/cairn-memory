-- Schema v3: an event's project must be its task's project. Applied once, in
-- its own transaction, by internal/store. Released migrations are never edited;
-- a change is a new numbered file.

-- events.project is caller-supplied and was not tied to tasks.project, so an
-- event written with another project's name would be invisible to ListEvents
-- and would be indexed under the wrong project by search. A task that does not
-- exist is left to the foreign key on task_id.
CREATE TRIGGER events_project_matches_task BEFORE INSERT ON events
WHEN EXISTS (SELECT 1 FROM tasks
              WHERE id = NEW.task_id AND project IS NOT NEW.project)
BEGIN
    SELECT RAISE(ABORT, 'event project differs from the project of its task');
END;
