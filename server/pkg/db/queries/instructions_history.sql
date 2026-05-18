-- name: InsertInstructionsHistory :one
INSERT INTO instructions_history (
    workspace_id,
    scope,
    member_id,
    content,
    actor_id,
    restored_from
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6
)
RETURNING *;

-- name: ListInstructionsHistory :many
SELECT
    ih.id,
    ih.workspace_id,
    ih.scope,
    ih.member_id,
    ih.content,
    ih.actor_id,
    ih.restored_from,
    ih.created_at,
    u.id AS actor_user_id,
    u.name AS actor_name,
    u.avatar_url AS actor_avatar_url
FROM instructions_history ih
LEFT JOIN member actor_member ON actor_member.id = ih.actor_id
LEFT JOIN "user" u ON u.id = actor_member.user_id
WHERE ih.workspace_id = $1
  AND ih.scope = $2
  AND (
      (ih.scope = 'personal' AND ih.member_id = $3)
      OR
      (ih.scope = 'system' AND ih.member_id IS NULL)
  )
ORDER BY ih.created_at DESC
LIMIT $4;

-- name: GetInstructionsHistory :one
SELECT
    ih.id,
    ih.workspace_id,
    ih.scope,
    ih.member_id,
    ih.content,
    ih.actor_id,
    ih.restored_from,
    ih.created_at,
    u.id AS actor_user_id,
    u.name AS actor_name,
    u.avatar_url AS actor_avatar_url
FROM instructions_history ih
LEFT JOIN member actor_member ON actor_member.id = ih.actor_id
LEFT JOIN "user" u ON u.id = actor_member.user_id
WHERE ih.id = $1
  AND ih.workspace_id = $2
  AND ih.scope = $3
  AND (
      (ih.scope = 'personal' AND ih.member_id = $4)
      OR
      (ih.scope = 'system' AND ih.member_id IS NULL)
  );
