-- Schema v5: review_by on directives (docs/DESIGN.md §3.4, decision D16). Applied
-- once, in its own transaction, by internal/store. Released migrations are never
-- edited; a change is a new numbered file.

-- review_by is the UTC time a directive is due to be reaffirmed or retired. A
-- directive past it is expired and left out of the capped set (CONTRACT §5).
-- Added with ALTER TABLE, so no row is dropped or rewritten beyond the backfill.
ALTER TABLE directives ADD COLUMN review_by TEXT;

-- Backfill: each active directive is due 90 days after ratified_at, the column
-- every ratify sets. The stored form is fixed width (store/codec.go), so the
-- first 19 characters are the whole seconds and the rest is restored as a
-- zero-nanosecond suffix. A directive with no ratified_at has no time to count
-- from; it is due 90 days after this migration runs, so the upgrade itself never
-- expires it. Retired directives keep NULL.
UPDATE directives
SET review_by = CASE
        WHEN ratified_at IS NOT NULL
            THEN strftime('%Y-%m-%dT%H:%M:%S', substr(ratified_at, 1, 19), '+90 days') || '.000000000Z'
        ELSE strftime('%Y-%m-%dT%H:%M:%S', 'now', '+90 days') || '.000000000Z'
    END
WHERE status = 'active';
