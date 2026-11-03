-- The leader briefing's "Initiatives your squad owns" block (spec 3.5). Every
-- query filters by workspace_id.

-- name: ListSquadOwnedInitiatives :many
-- Active or operating initiatives whose current stage this squad owns. The stage
-- name comes from the pipeline's stages; a stage the pipeline no longer names
-- falls back to its key.
SELECT
    i.seq,
    i.title,
    COALESCE(stage.name, i.stage_key)::text AS stage_name,
    GREATEST(0, FLOOR(EXTRACT(EPOCH FROM (sqlc.arg('now')::timestamptz - i.stage_entered_at)) / 86400))::integer AS days_in_stage
FROM initiative i
LEFT JOIN pipeline p ON p.id = i.pipeline_id AND p.workspace_id = i.workspace_id
LEFT JOIN LATERAL (
    SELECT s->>'name' AS name
    FROM jsonb_array_elements(p.stages) AS s
    WHERE s->>'key' = i.stage_key
    LIMIT 1
) stage ON true
WHERE i.workspace_id = sqlc.arg('workspace_id')::uuid
  AND i.owner_squad_id = sqlc.arg('squad_id')::uuid
  AND i.status IN ('active', 'operating')
ORDER BY i.seq ASC
LIMIT 50;
