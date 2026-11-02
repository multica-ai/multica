-- Renumbered from 458 to avoid the cancellation-provenance migration.
-- IF NOT EXISTS preserves an index already created under the old version;
-- the runner records this version without rewriting the historical ledger.
-- CountAssigneeChangesByActor filters by workspace and member actor, then
-- groups on the assignment target stored in details. Keep the expressions in
-- key order so PostgreSQL can aggregate the matching history directly from
-- this targeted partial index instead of scanning the full activity log.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_activity_log_member_assignee_frequency
    ON activity_log (
        workspace_id,
        actor_id,
        (details->>'to_type'),
        (details->>'to_id')
    )
    WHERE actor_type = 'member'
      AND action = 'assignee_changed'
      AND details->>'to_type' IS NOT NULL
      AND details->>'to_id' IS NOT NULL;
