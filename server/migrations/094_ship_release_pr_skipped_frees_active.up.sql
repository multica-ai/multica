-- Free skipped PRs from their stranded "active" membership. The
-- merge-train skip path in v1 set merge_state='skipped' but never
-- touched is_active, which left the row matching the partial unique
-- index `(pull_request_id) WHERE is_active=TRUE`. That index then
-- blocked the same PR from being added to any new release — and the
-- PR card showed it as "still in the old release" even though that
-- release had shipped without it.
--
-- Going forward the service flips is_active=FALSE at the same time
-- it sets merge_state='skipped' (see release_merge.go). This
-- migration retroactively frees rows already in the bad state so
-- the user can add those PRs to fresh releases without manual SQL.
--
-- merge_state='failed' rows are NOT freed here — failed PRs are
-- recoverable (the operator can fix the conflict and Resume the
-- train), so the "currently locked into release X" relationship
-- still holds.
UPDATE ship_release_pull_request
SET is_active = FALSE
WHERE merge_state = 'skipped'
  AND is_active   = TRUE;
