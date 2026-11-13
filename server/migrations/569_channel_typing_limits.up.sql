ALTER TABLE channel_typing_reaction ADD COLUMN IF NOT EXISTS abandoned_at timestamptz;
ALTER TABLE channel_typing_reaction ADD COLUMN IF NOT EXISTS quota_slot integer;

-- Give existing pending rows a bounded share of each workspace's quota.
-- Excess rows become explicit failed outcomes; never retain their credentials.
WITH ranked AS (
 SELECT id, row_number() OVER (PARTITION BY workspace_id ORDER BY created_at DESC, id) AS slot
 FROM channel_typing_reaction WHERE cleaned_at IS NULL AND abandoned_at IS NULL
)
UPDATE channel_typing_reaction r SET
 quota_slot = CASE WHEN ranked.slot <= 1000 THEN ranked.slot::integer END,
 abandoned_at = CASE WHEN ranked.slot > 1000 THEN now() END,
 cleanup_required = cleanup_required OR ranked.slot > 1000,
 installation_snapshot = CASE WHEN ranked.slot > 1000 THEN '{}'::jsonb ELSE installation_snapshot END
FROM ranked WHERE r.id = ranked.id;

UPDATE channel_typing_reaction SET installation_snapshot = '{}'::jsonb
WHERE cleaned_at IS NOT NULL;
