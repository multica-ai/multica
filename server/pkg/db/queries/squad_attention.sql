-- A squad's adaptive attention caps (see migration 718). Every query filters by
-- workspace_id; there are no foreign keys.

-- name: GetSquadAttention :one
SELECT * FROM squad_attention
WHERE squad_id = $1 AND workspace_id = $2;

-- name: ListSquadDecisionCountsSince :many
-- Decided proposals per squad since the given time: approved, rejected and
-- expired. A proposal that is revising or pending is not a decision.
SELECT
    workspace_id,
    squad_id,
    count(*) FILTER (WHERE state = 'approved')::bigint AS approved,
    count(*) FILTER (WHERE state = 'rejected')::bigint AS rejected,
    count(*) FILTER (WHERE state = 'expired')::bigint AS expired
FROM proposal
WHERE decided_at >= sqlc.arg('since')::timestamptz
  AND state IN ('approved', 'rejected', 'expired')
GROUP BY workspace_id, squad_id;

-- name: UpsertComputedSquadAttention :exec
-- Writes the computed caps. The pin columns are not touched: a pin survives the
-- nightly recompute.
INSERT INTO squad_attention (
    squad_id, workspace_id, pending_cap, weekly_cap, acceptance_rate, sample_size, computed_at
) VALUES (
    $1, $2, $3, $4, $5, $6, now()
)
ON CONFLICT (squad_id) DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    pending_cap = EXCLUDED.pending_cap,
    weekly_cap = EXCLUDED.weekly_cap,
    acceptance_rate = EXCLUDED.acceptance_rate,
    sample_size = EXCLUDED.sample_size,
    computed_at = now();

-- name: ResetComputedSquadAttentionExcept :execrows
-- A squad whose sample fell below the minimum keeps the defaults, so its rows
-- go back to them. Squads named in squad_ids are left to the upserts.
UPDATE squad_attention
SET pending_cap = sqlc.arg('pending_cap'),
    weekly_cap = sqlc.arg('weekly_cap'),
    acceptance_rate = NULL,
    sample_size = 0,
    computed_at = now()
WHERE NOT (squad_id = ANY(sqlc.arg('squad_ids')::uuid[]))
  AND (pending_cap <> sqlc.arg('pending_cap') OR weekly_cap <> sqlc.arg('weekly_cap')
       OR sample_size <> 0 OR acceptance_rate IS NOT NULL);

-- name: UpsertSquadAttentionPin :one
-- Sets or clears a pin. A squad with no row yet gets the default computed caps
-- from the column defaults.
INSERT INTO squad_attention (
    squad_id, workspace_id, pinned, pinned_pending_cap, pinned_weekly_cap
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (squad_id) DO UPDATE SET
    pinned = EXCLUDED.pinned,
    pinned_pending_cap = EXCLUDED.pinned_pending_cap,
    pinned_weekly_cap = EXCLUDED.pinned_weekly_cap
RETURNING *;

-- name: DeleteWorkspaceSquadAttention :exec
-- Workspace teardown: attention rows have no foreign keys to cascade.
DELETE FROM squad_attention WHERE workspace_id = $1;
