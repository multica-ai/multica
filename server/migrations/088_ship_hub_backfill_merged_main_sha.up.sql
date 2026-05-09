-- Backfill merged_main_sha for releases that completed their merge train
-- BEFORE Phase 7c's migration ran. Without this column populated, the
-- staging-deploy webhook can't reverse-look-up which release a deploy
-- belongs to, so the release sits in_staging forever with no deploy
-- linkage. Affects only releases created during the Phase 7a/7b
-- window before 7c landed.
--
-- Idempotent: WHERE merged_main_sha IS NULL OR merged_main_sha = '' so
-- repeated runs are no-ops.
UPDATE ship_release sr
SET merged_main_sha = (
    SELECT srpr.merged_sha
    FROM ship_release_pull_request srpr
    WHERE srpr.release_id = sr.id
      AND srpr.merge_state = 'merged'
      AND srpr.merged_sha IS NOT NULL
      AND srpr.merged_sha <> ''
    ORDER BY srpr.position DESC
    LIMIT 1
)
WHERE sr.merged_main_sha IS NULL OR sr.merged_main_sha = '';
