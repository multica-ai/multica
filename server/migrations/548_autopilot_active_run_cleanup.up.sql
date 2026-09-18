-- Scheduled runs are single-flight per Autopilot. Before the unique index is
-- installed, close historical duplicate active rows and release their quota
-- reservations so the index build cannot fail on existing data.
WITH ranked AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY autopilot_id
            ORDER BY created_at DESC, id DESC
        ) AS occurrence_rank
    FROM autopilot_run
    WHERE source = 'schedule'
      AND status IN ('issue_created', 'running')
), reclaimed AS (
    UPDATE autopilot_run AS ar
    SET status = 'skipped',
        completed_at = COALESCE(ar.completed_at, now()),
        failure_reason = COALESCE(
            ar.failure_reason,
            'deduplicated duplicate active scheduled run during single-flight migration'
        ),
        reason_code = 'already_active'
    FROM ranked
    WHERE ar.id = ranked.id
      AND ranked.occurrence_rank > 1
    RETURNING ar.quota_reservation_id
), released AS (
    UPDATE autopilot_quota_reservation AS qr
    SET state = 'released',
        finalized_at = COALESCE(qr.finalized_at, now())
    FROM reclaimed
    WHERE qr.id = reclaimed.quota_reservation_id
      AND qr.state = 'reserved'
    RETURNING qr.workspace_id, qr.period_start, qr.period_end
), release_counts AS (
    SELECT workspace_id, period_start, period_end, COUNT(*) AS released_count
    FROM released
    GROUP BY workspace_id, period_start, period_end
)
UPDATE autopilot_quota_period AS qp
SET reserved_count = GREATEST(qp.reserved_count - release_counts.released_count, 0),
    updated_at = now()
FROM release_counts
WHERE qp.workspace_id = release_counts.workspace_id
  AND qp.period_start = release_counts.period_start
  AND qp.period_end = release_counts.period_end;
