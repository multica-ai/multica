-- name: LockIssueForReactionWrite :one
-- Take the issue, in the mode of the revision bump, before adding or removing
-- a reaction. A second add of the same reaction then waits for the first to
-- commit and starts with a snapshot that sees its row; without this it waits
-- inside AddIssueReaction, whose snapshot predates that row, and returns none.
-- Removes take it too, so an add and a remove of the same reaction lock the
-- issue and the reaction row in the same order and cannot deadlock.
SELECT id FROM issue
WHERE id = $1 AND workspace_id = $2
FOR NO KEY UPDATE;

-- name: AddIssueReaction :one
WITH inserted AS (
    INSERT INTO issue_reaction (issue_id, workspace_id, actor_type, actor_id, emoji)
    VALUES ($1, $2, $3, $4, $5)
    ON CONFLICT (issue_id, actor_type, actor_id, emoji) DO NOTHING
    RETURNING *
), bumped AS (
    UPDATE issue
    SET revision = revision + 1
    WHERE id IN (SELECT issue_id FROM inserted)
    RETURNING revision
)
SELECT reaction.*, COALESCE((SELECT revision FROM bumped), 0)::bigint AS issue_revision
FROM inserted reaction
UNION ALL
SELECT reaction.*, 0::bigint AS issue_revision
FROM issue_reaction reaction
WHERE reaction.issue_id = $1
  AND reaction.actor_type = $3
  AND reaction.actor_id = $4
  AND reaction.emoji = $5
  AND NOT EXISTS (SELECT 1 FROM inserted)
LIMIT 1;

-- name: RemoveIssueReaction :one
WITH deleted AS (
    DELETE FROM issue_reaction
    WHERE issue_id = $1 AND actor_type = $2 AND actor_id = $3 AND emoji = $4
    RETURNING issue_id
), bumped AS (
    UPDATE issue
    SET revision = revision + 1
    WHERE id IN (SELECT issue_id FROM deleted)
    RETURNING revision
)
SELECT EXISTS(SELECT 1 FROM deleted) AS changed,
       COALESCE((SELECT revision FROM bumped), 0)::bigint AS issue_revision;

-- name: ListIssueReactions :many
SELECT * FROM issue_reaction
WHERE issue_id = $1
ORDER BY created_at ASC;
