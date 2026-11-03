-- Schema v6: the web UI's sessions and directive reaffirmations (docs/DESIGN.md
-- §3.4, §3.6, §5.5; decisions D23, D26). Applied once, in its own transaction, by
-- internal/store. Released migrations are never edited; a change is a new
-- numbered file.

-- ui_sessions holds one row per signed-in browser. The cookie carries the token;
-- the row carries only its SHA-256, in hex (auth.HashToken), so a copy of the
-- database cannot sign anyone in. The role is not stored: it is the key's scope,
-- read from api_keys on each request. Timestamps use the stored form of
-- store/codec.go, which is fixed width, so text comparison is time order.
-- expires_at is the absolute end (created_at + 12 hours, not sliding). last_seen_at
-- drives the idle end (last_seen_at + 2 hours).
CREATE TABLE ui_sessions (
    token_hash    TEXT PRIMARY KEY,
    key_id        INTEGER NOT NULL REFERENCES api_keys(id),
    created_at    TEXT NOT NULL,
    expires_at    TEXT NOT NULL,
    last_seen_at  TEXT NOT NULL
);
-- The cleanup deletes by expires_at, and a revoked key's sessions are found by key_id.
CREATE INDEX ui_sessions_expires_at ON ui_sessions(expires_at);
CREATE INDEX ui_sessions_key_id ON ui_sessions(key_id);

-- directive_reaffirmations holds one row per DirectiveReaffirm (CONTRACT §8, D26),
-- written in the same transaction as the change to directives.review_by. The
-- previous and new review_by are timestamps in the stored form, as directives holds
-- them: midnight UTC of the named day.
CREATE TABLE directive_reaffirmations (
    id                  INTEGER PRIMARY KEY,
    directive_id        INTEGER NOT NULL REFERENCES directives(id),
    previous_review_by  TEXT NOT NULL,
    review_by           TEXT NOT NULL,
    reaffirmed_by       TEXT NOT NULL,
    reaffirmed_at       TEXT NOT NULL
);
