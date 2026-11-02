-- Schema v4: API keys (docs/DESIGN.md §3.6, §8). Applied once, in its own
-- transaction, by internal/store. Released migrations are never edited; a change
-- is a new numbered file.

-- v1 created api_keys with a scopes column and none of the constraints below.
-- No code has ever written it, so it is empty on every database. This migration
-- replaces it with the M2 shape rather than editing v1. The assert first: a
-- table holding any row aborts the migration, so no key row is dropped unseen.
-- The assert is a CHECK on a one-row table, which fails when the count is not 0. The
-- column is named for the cause, so the error reads "CHECK constraint failed:
-- api_keys_must_be_empty = 0" and an operator can see why the migration stopped.
CREATE TABLE api_keys_assert_empty (api_keys_must_be_empty INTEGER CHECK (api_keys_must_be_empty = 0));
INSERT INTO api_keys_assert_empty SELECT COUNT(*) FROM api_keys;
DROP TABLE api_keys_assert_empty;
DROP TABLE api_keys;

-- §3.6 api_keys. A key is stored only as its hash (auth.HashKey); the plaintext
-- is shown once, when the key is made, and is never kept. A key has one scope:
-- agent (the MCP tools), operator (decisions and ratification), or viewer
-- (read-only). A revoked key keeps its row, with revoked_at set, so its name stays
-- taken and the history is kept. The name is unique for a revoked key too.
CREATE TABLE api_keys (
    id            INTEGER PRIMARY KEY,
    name          TEXT NOT NULL UNIQUE,
    hash          TEXT NOT NULL UNIQUE,
    scope         TEXT NOT NULL CHECK (scope IN ('agent', 'operator', 'viewer')),
    created_at    TEXT NOT NULL,
    last_used_at  TEXT,
    revoked_at    TEXT
);
