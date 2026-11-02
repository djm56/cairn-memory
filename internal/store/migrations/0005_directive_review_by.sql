-- Schema v5: review_by on directives (docs/DESIGN.md §3.4, decision D16). Applied
-- once, in its own transaction, by internal/store. Released migrations are never
-- edited; a change is a new numbered file.

-- review_by is the UTC time a directive is due to be reaffirmed or retired. A
-- directive past it is expired and left out of the capped set (CONTRACT §5).
-- Added with ALTER TABLE, so no row is dropped or rewritten beyond the backfill.
ALTER TABLE directives ADD COLUMN review_by TEXT;

-- Backfill: every active directive is due 90 days after this migration runs, not
-- after its ratified_at. A directive ratified long before the upgrade would
-- otherwise be expired the moment this migration applied, and M2 has no in-place
-- way to reaffirm one (CONTRACT §8: retire and ratify again). So the upgrade
-- itself never expires a directive. The stored form is fixed width
-- (store/codec.go): the zero-nanosecond suffix is written out. Retired directives
-- keep NULL.
UPDATE directives
SET review_by = strftime('%Y-%m-%dT%H:%M:%S', 'now', '+90 days') || '.000000000Z'
WHERE status = 'active';
