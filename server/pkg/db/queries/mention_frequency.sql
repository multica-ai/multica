-- name: UpsertMentionFrequency :exec
INSERT INTO mention_frequency (
  workspace_id,
  actor_type,
  actor_id,
  mentioned_by,
  frequency,
  last_mentioned_at,
  updated_at
)
VALUES ($1, $2, $3, $4, 1, now(), now())
ON CONFLICT (workspace_id, actor_type, actor_id, mentioned_by)
DO UPDATE SET
  frequency = mention_frequency.frequency + 1,
  last_mentioned_at = now(),
  updated_at = now();

-- name: ListMentionFrequencyByUser :many
SELECT workspace_id, actor_type, actor_id, mentioned_by, frequency, last_mentioned_at
FROM mention_frequency
WHERE workspace_id = $1 AND mentioned_by = $2
ORDER BY last_mentioned_at DESC, frequency DESC;
