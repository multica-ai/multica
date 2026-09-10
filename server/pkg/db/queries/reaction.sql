-- name: AddReaction :one
WITH inserted AS (
    INSERT INTO comment_reaction (comment_id, workspace_id, actor_type, actor_id, emoji)
    VALUES ($1, $2, $3, $4, $5)
    ON CONFLICT (comment_id, actor_type, actor_id, emoji) DO NOTHING
    RETURNING *
), bumped AS (
    UPDATE comment
    SET revision = revision + 1
    WHERE id IN (SELECT comment_id FROM inserted)
    RETURNING revision
)
SELECT reaction.*, COALESCE((SELECT revision FROM bumped), 0)::bigint AS comment_revision
FROM inserted reaction
UNION ALL
SELECT reaction.*, 0::bigint AS comment_revision
FROM comment_reaction reaction
WHERE reaction.comment_id = $1
  AND reaction.actor_type = $3
  AND reaction.actor_id = $4
  AND reaction.emoji = $5
  AND NOT EXISTS (SELECT 1 FROM inserted)
LIMIT 1;

-- name: RemoveReaction :one
WITH deleted AS (
    DELETE FROM comment_reaction
    WHERE comment_id = $1 AND actor_type = $2 AND actor_id = $3 AND emoji = $4
    RETURNING comment_id
), bumped AS (
    UPDATE comment
    SET revision = revision + 1
    WHERE id IN (SELECT comment_id FROM deleted)
    RETURNING revision
)
SELECT EXISTS(SELECT 1 FROM deleted) AS changed,
       COALESCE((SELECT revision FROM bumped), 0)::bigint AS comment_revision;

-- name: ListReactionsByCommentIDs :many
SELECT * FROM comment_reaction
WHERE comment_id = ANY($1::uuid[])
ORDER BY created_at ASC;
