-- name: CreateVIBESCLIPATBinding :one
INSERT INTO vibes_cli_pat_binding (
    pat_id, multica_user_id, multica_workspace_id,
    vibes_user_id, vibes_session_id, vibes_workspace_id, tag_session_id,
    account_epoch, session_workspace_generation, authority_version,
    membership_generation, session_expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetVIBESCLIPATBindingByTokenHash :one
SELECT binding.*
FROM vibes_cli_pat_binding AS binding
JOIN personal_access_token AS pat ON pat.id = binding.pat_id
WHERE pat.token_hash = $1
  AND pat.revoked = FALSE
  AND (pat.expires_at IS NULL OR pat.expires_at > now());
