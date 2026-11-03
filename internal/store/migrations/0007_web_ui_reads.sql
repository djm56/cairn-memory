-- Schema v7: indexes for the web UI's reads (docs/DESIGN.md §5.5, CONTRACT §8.1).
-- Applied once, in its own transaction, by internal/store. Released migrations are
-- never edited; a change is a new numbered file. No table or column changes: every
-- statement here is an index.

-- tasks across projects, for the dashboard and task_browse. task_browse without a
-- status lists every task, most recently updated first (keyset on updated_at, id).
-- idx_tasks_project_updated (migration 0002) starts with project, so it does not serve
-- a list across projects.
CREATE INDEX idx_tasks_updated ON tasks (updated_at, id);

-- tasks by status, across projects: the running list (status active, most recently
-- updated first) and the blocked count of the dashboard.
CREATE INDEX idx_tasks_status_updated ON tasks (status, updated_at, id);

-- tasks by close time: the 30-day figures and the six monthly rows of the dashboard
-- read the done and failed tasks whose closed_at falls in a window.
CREATE INDEX idx_tasks_status_closed ON tasks (status, closed_at);

-- learnings by status, across projects: the dashboard's proposed count.
-- idx_learnings_project_status (migration 0002) starts with project.
CREATE INDEX idx_learnings_status ON learnings (status);

-- directives by status and review_by: the dashboard's expired count and oldest
-- review_by, and the order of directive_browse for active directives.
CREATE INDEX idx_directives_status_review ON directives (status, review_by);
