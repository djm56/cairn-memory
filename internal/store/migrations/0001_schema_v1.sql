-- Schema v1: docs/DESIGN.md §3. Applied once, in its own transaction, by
-- internal/store. Released migrations are never edited; a change is a new
-- numbered file.

-- §3.6 projects and sessions. Every other table carries a project.
CREATE TABLE projects (
    name          TEXT PRIMARY KEY,
    display_name  TEXT,
    created_at    TEXT NOT NULL,
    last_seen_at  TEXT NOT NULL,
    settings      TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE sessions (
    id            TEXT PRIMARY KEY,
    project       TEXT NOT NULL REFERENCES projects(name),
    client        TEXT,
    agent         TEXT,
    started_at    TEXT NOT NULL,
    last_seen_at  TEXT NOT NULL
);

-- §3.1 tasks. no_lessons_reason is set at close, with an empty lesson list
-- (§3.7; CONTRACT §2).
CREATE TABLE tasks (
    id                TEXT PRIMARY KEY,
    project           TEXT NOT NULL REFERENCES projects(name),
    title             TEXT NOT NULL,
    goal              TEXT NOT NULL,
    context           TEXT,
    constraints       TEXT,
    status            TEXT NOT NULL CHECK (status IN
                        ('open', 'active', 'blocked', 'done', 'failed', 'abandoned')),
    priority          TEXT NOT NULL CHECK (priority IN
                        ('low', 'normal', 'high', 'urgent')),
    owner             TEXT,
    next_action       TEXT,
    blockers          TEXT,
    outcome_summary   TEXT,
    no_lessons_reason TEXT,
    opened_at         TEXT NOT NULL,
    started_at        TEXT,
    closed_at         TEXT,
    updated_at        TEXT NOT NULL
);

CREATE TABLE task_criteria (
    task_id    TEXT NOT NULL REFERENCES tasks(id),
    ord        INTEGER NOT NULL,
    criterion  TEXT NOT NULL,
    result     TEXT CHECK (result IS NULL OR result IN
                 ('met', 'partial', 'unmet', 'unclear')),
    evidence   TEXT,
    PRIMARY KEY (task_id, ord)
);

-- Key files (from the brief) and deliverables (at close) share this table,
-- so the primary key includes role: both may have an ord 1.
CREATE TABLE task_files (
    task_id  TEXT NOT NULL REFERENCES tasks(id),
    role     TEXT NOT NULL CHECK (role IN ('key', 'deliverable')),
    ord      INTEGER NOT NULL,
    path     TEXT NOT NULL,
    note     TEXT,
    PRIMARY KEY (task_id, role, ord)
);

CREATE TABLE task_lessons (
    task_id     TEXT NOT NULL REFERENCES tasks(id),
    ord         INTEGER NOT NULL,
    kind        TEXT NOT NULL CHECK (kind IN
                  ('wrong-assumption', 'reuse', 'similar-task')),
    text        TEXT NOT NULL,
    why         TEXT,
    correction  TEXT,
    PRIMARY KEY (task_id, ord)
);

CREATE TABLE task_mistakes (
    task_id     TEXT NOT NULL REFERENCES tasks(id),
    ord         INTEGER NOT NULL,
    agent       TEXT NOT NULL,
    mistake     TEXT NOT NULL,
    impact      TEXT NOT NULL CHECK (impact IN ('low', 'med', 'high')),
    correction  TEXT,
    prevention  TEXT,
    PRIMARY KEY (task_id, ord)
);

-- §3.2 steps. position is a real number so steps are inserted between
-- others without renumbering.
CREATE TABLE steps (
    id          INTEGER PRIMARY KEY,
    task_id     TEXT NOT NULL REFERENCES tasks(id),
    position    REAL NOT NULL,
    title       TEXT NOT NULL,
    agent       TEXT,
    status      TEXT NOT NULL CHECK (status IN
                  ('pending', 'active', 'done', 'failed', 'skipped')),
    attempts    INTEGER NOT NULL DEFAULT 0,
    summary     TEXT,
    started_at  TEXT,
    ended_at    TEXT
);

-- §3.3 events (append-only by tool contract, CONTRACT §4) and reviews.
CREATE TABLE events (
    id           INTEGER PRIMARY KEY,
    task_id      TEXT NOT NULL REFERENCES tasks(id),
    project      TEXT NOT NULL REFERENCES projects(name),
    step_id      INTEGER REFERENCES steps(id),
    agent        TEXT NOT NULL,
    session_id   TEXT REFERENCES sessions(id),
    kind         TEXT NOT NULL CHECK (kind IN
                   ('note', 'started', 'finished', 'blocked', 'unblocked',
                    'handoff', 'decision', 'review', 'close', 'custom')),
    text         TEXT NOT NULL,
    data         TEXT,
    occurred_at  TEXT NOT NULL
);

CREATE TABLE reviews (
    id          INTEGER PRIMARY KEY,
    task_id     TEXT NOT NULL REFERENCES tasks(id),
    step_id     INTEGER NOT NULL REFERENCES steps(id),
    reviewer    TEXT NOT NULL,
    verdict     TEXT NOT NULL CHECK (verdict IN ('approved', 'not-approved')),
    critical    INTEGER NOT NULL,
    warnings    INTEGER NOT NULL,
    summary     TEXT,
    created_at  TEXT NOT NULL
);

-- §3.4 learnings and directives. Both carry the operator's decision fields.
CREATE TABLE learnings (
    id             INTEGER PRIMARY KEY,
    project        TEXT NOT NULL REFERENCES projects(name),
    task_id        TEXT REFERENCES tasks(id),
    agent          TEXT NOT NULL,
    kind           TEXT NOT NULL CHECK (kind IN ('finding', 'pattern', 'agent-note')),
    title          TEXT NOT NULL,
    body           TEXT NOT NULL,
    target         TEXT,
    rationale      TEXT,
    example        TEXT,
    status         TEXT NOT NULL CHECK (status IN
                     ('proposed', 'accepted', 'rejected', 'applied',
                      'retired', 'superseded')),
    superseded_by  INTEGER REFERENCES learnings(id),
    decided_by     TEXT,
    decided_at     TEXT,
    decision_note  TEXT,
    created_at     TEXT NOT NULL
);

CREATE TABLE directives (
    id               INTEGER PRIMARY KEY,
    code             TEXT NOT NULL UNIQUE,
    scope            TEXT NOT NULL CHECK (scope IN ('global', 'project', 'agent')),
    project          TEXT REFERENCES projects(name),
    agent            TEXT,
    title            TEXT NOT NULL,
    applies_when     TEXT,
    rule             TEXT NOT NULL,
    rationale        TEXT,
    reviewer_check   TEXT,
    example          TEXT,
    status           TEXT NOT NULL CHECK (status IN ('active', 'retired')),
    source_learning  INTEGER REFERENCES learnings(id),
    ratified_by      TEXT,
    ratified_at      TEXT,
    retired_at       TEXT
);

-- §3.5 notes.
CREATE TABLE notes (
    id          INTEGER PRIMARY KEY,
    task_id     TEXT REFERENCES tasks(id),
    project     TEXT NOT NULL REFERENCES projects(name),
    title       TEXT NOT NULL,
    body        TEXT NOT NULL,
    created_by  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

-- §3.6 api keys (server mode). The hash is stored, never the key.
CREATE TABLE api_keys (
    id            INTEGER PRIMARY KEY,
    name          TEXT NOT NULL,
    hash          TEXT NOT NULL,
    scopes        TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    last_used_at  TEXT
);

-- §3.5 search. One FTS5 table over every searchable source. kind names the
-- source table; ref is its key as text (for lessons and mistakes,
-- "task-id/ord"). Rows are kept in step by the triggers below.
CREATE VIRTUAL TABLE search_index USING fts5(
    kind UNINDEXED,
    ref UNINDEXED,
    project UNINDEXED,
    task_id UNINDEXED,
    title,
    body
);

-- Triggers. Each source table has one trigger per change. Update and delete
-- remove the row by (kind, ref), which scans the index; accepted at v1 scale.

-- tasks: title, goal, context, outcome.
CREATE TRIGGER search_tasks_ai AFTER INSERT ON tasks BEGIN
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('task', NEW.id, NEW.project, NEW.id, NEW.title,
            NEW.goal || ' ' || coalesce(NEW.context, '') || ' '
              || coalesce(NEW.outcome_summary, ''));
END;
CREATE TRIGGER search_tasks_au AFTER UPDATE ON tasks BEGIN
    DELETE FROM search_index WHERE kind = 'task' AND ref = OLD.id;
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('task', NEW.id, NEW.project, NEW.id, NEW.title,
            NEW.goal || ' ' || coalesce(NEW.context, '') || ' '
              || coalesce(NEW.outcome_summary, ''));
END;
CREATE TRIGGER search_tasks_ad AFTER DELETE ON tasks BEGIN
    DELETE FROM search_index WHERE kind = 'task' AND ref = OLD.id;
END;

-- events: text.
CREATE TRIGGER search_events_ai AFTER INSERT ON events BEGIN
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('event', CAST(NEW.id AS TEXT), NEW.project, NEW.task_id,
            '', NEW.text);
END;
CREATE TRIGGER search_events_au AFTER UPDATE ON events BEGIN
    DELETE FROM search_index WHERE kind = 'event' AND ref = CAST(OLD.id AS TEXT);
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('event', CAST(NEW.id AS TEXT), NEW.project, NEW.task_id,
            '', NEW.text);
END;
CREATE TRIGGER search_events_ad AFTER DELETE ON events BEGIN
    DELETE FROM search_index WHERE kind = 'event' AND ref = CAST(OLD.id AS TEXT);
END;

-- learnings: title, body, rationale.
CREATE TRIGGER search_learnings_ai AFTER INSERT ON learnings BEGIN
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('learning', CAST(NEW.id AS TEXT), NEW.project, NEW.task_id,
            NEW.title, NEW.body || ' ' || coalesce(NEW.rationale, ''));
END;
CREATE TRIGGER search_learnings_au AFTER UPDATE ON learnings BEGIN
    DELETE FROM search_index WHERE kind = 'learning' AND ref = CAST(OLD.id AS TEXT);
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('learning', CAST(NEW.id AS TEXT), NEW.project, NEW.task_id,
            NEW.title, NEW.body || ' ' || coalesce(NEW.rationale, ''));
END;
CREATE TRIGGER search_learnings_ad AFTER DELETE ON learnings BEGIN
    DELETE FROM search_index WHERE kind = 'learning' AND ref = CAST(OLD.id AS TEXT);
END;

-- directives: title, rule. Directives are global or scoped; project is
-- NULL for a global directive.
CREATE TRIGGER search_directives_ai AFTER INSERT ON directives BEGIN
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('directive', CAST(NEW.id AS TEXT), NEW.project, NULL,
            NEW.title, NEW.rule);
END;
CREATE TRIGGER search_directives_au AFTER UPDATE ON directives BEGIN
    DELETE FROM search_index WHERE kind = 'directive' AND ref = CAST(OLD.id AS TEXT);
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('directive', CAST(NEW.id AS TEXT), NEW.project, NULL,
            NEW.title, NEW.rule);
END;
CREATE TRIGGER search_directives_ad AFTER DELETE ON directives BEGIN
    DELETE FROM search_index WHERE kind = 'directive' AND ref = CAST(OLD.id AS TEXT);
END;

-- notes: title, body.
CREATE TRIGGER search_notes_ai AFTER INSERT ON notes BEGIN
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('note', CAST(NEW.id AS TEXT), NEW.project, NEW.task_id,
            NEW.title, NEW.body);
END;
CREATE TRIGGER search_notes_au AFTER UPDATE ON notes BEGIN
    DELETE FROM search_index WHERE kind = 'note' AND ref = CAST(OLD.id AS TEXT);
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('note', CAST(NEW.id AS TEXT), NEW.project, NEW.task_id,
            NEW.title, NEW.body);
END;
CREATE TRIGGER search_notes_ad AFTER DELETE ON notes BEGIN
    DELETE FROM search_index WHERE kind = 'note' AND ref = CAST(OLD.id AS TEXT);
END;

-- lessons: text, why, correction. The project comes from the task.
CREATE TRIGGER search_lessons_ai AFTER INSERT ON task_lessons BEGIN
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('lesson', NEW.task_id || '/' || NEW.ord,
            (SELECT project FROM tasks WHERE id = NEW.task_id), NEW.task_id,
            NEW.text, coalesce(NEW.why, '') || ' ' || coalesce(NEW.correction, ''));
END;
CREATE TRIGGER search_lessons_au AFTER UPDATE ON task_lessons BEGIN
    DELETE FROM search_index
     WHERE kind = 'lesson' AND ref = OLD.task_id || '/' || OLD.ord;
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('lesson', NEW.task_id || '/' || NEW.ord,
            (SELECT project FROM tasks WHERE id = NEW.task_id), NEW.task_id,
            NEW.text, coalesce(NEW.why, '') || ' ' || coalesce(NEW.correction, ''));
END;
CREATE TRIGGER search_lessons_ad AFTER DELETE ON task_lessons BEGIN
    DELETE FROM search_index
     WHERE kind = 'lesson' AND ref = OLD.task_id || '/' || OLD.ord;
END;

-- mistakes: mistake, correction, prevention. The project comes from the task.
CREATE TRIGGER search_mistakes_ai AFTER INSERT ON task_mistakes BEGIN
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('mistake', NEW.task_id || '/' || NEW.ord,
            (SELECT project FROM tasks WHERE id = NEW.task_id), NEW.task_id,
            NEW.mistake, coalesce(NEW.correction, '') || ' '
              || coalesce(NEW.prevention, ''));
END;
CREATE TRIGGER search_mistakes_au AFTER UPDATE ON task_mistakes BEGIN
    DELETE FROM search_index
     WHERE kind = 'mistake' AND ref = OLD.task_id || '/' || OLD.ord;
    INSERT INTO search_index (kind, ref, project, task_id, title, body)
    VALUES ('mistake', NEW.task_id || '/' || NEW.ord,
            (SELECT project FROM tasks WHERE id = NEW.task_id), NEW.task_id,
            NEW.mistake, coalesce(NEW.correction, '') || ' '
              || coalesce(NEW.prevention, ''));
END;
CREATE TRIGGER search_mistakes_ad AFTER DELETE ON task_mistakes BEGIN
    DELETE FROM search_index
     WHERE kind = 'mistake' AND ref = OLD.task_id || '/' || OLD.ord;
END;
