-- name: CreateServiceAccountUser :one
-- Machine identity (kind='service'). The email is generated, unique, and not
-- a reachable mailbox; service accounts never receive login codes.
INSERT INTO "user" (name, email, kind)
VALUES (sqlc.arg(name), 'service-' || gen_random_uuid() || '@' || sqlc.arg(workspace_slug) || '.multica.service', 'service')
RETURNING *;

-- name: GetServiceAccountMember :one
-- The service account must be a member of the caller's workspace, so token
-- operations can't touch accounts in other workspaces even when a caller
-- guesses an account ID.
SELECT m.*, u.kind
FROM member m
JOIN "user" u ON u.id = m.user_id
WHERE m.user_id = $1
  AND m.workspace_id = $2
  AND u.kind = 'service';

-- name: ListServiceAccountsByWorkspace :many
SELECT u.id, u.name, u.email, u.created_at,
       COALESCE(t.active_token_count, 0)::bigint AS active_token_count,
       t.last_used_at::timestamptz AS last_used_at
FROM "user" u
JOIN member m ON m.user_id = u.id
LEFT JOIN LATERAL (
    SELECT count(*) AS active_token_count, max(p.last_used_at) AS last_used_at
    FROM personal_access_token p
    WHERE p.user_id = u.id
      AND p.revoked = FALSE
      AND (p.expires_at IS NULL OR p.expires_at > now())
) t ON TRUE
WHERE m.workspace_id = $1
  AND u.kind = 'service'
ORDER BY u.created_at ASC, u.id ASC;

-- name: RevokeServiceAccountToken :one
-- Admin revocation for service-account tokens. The EXISTS clause makes the
-- revoke a no-op unless the token belongs to a service account that is
-- still a member of the caller's workspace.
UPDATE personal_access_token p
SET revoked = TRUE
WHERE p.id = $1
  AND p.revoked = FALSE
  AND EXISTS (
      SELECT 1 FROM member m
      JOIN "user" u ON u.id = m.user_id
      WHERE m.user_id = p.user_id
        AND m.workspace_id = $2
        AND u.kind = 'service'
  )
RETURNING p.token_hash;
