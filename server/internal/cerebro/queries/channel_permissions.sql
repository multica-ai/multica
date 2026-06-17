-- name: GetChannelPermissions :one
SELECT channel_id, rename_policy, add_members_policy, allow_self_leave, updated_at
FROM cerebro_channel_permissions
WHERE channel_id = $1;

-- name: UpsertChannelPermissions :one
INSERT INTO cerebro_channel_permissions (channel_id, rename_policy, add_members_policy, allow_self_leave, updated_at)
VALUES ($1, $2, $3, $4, NOW())
ON CONFLICT (channel_id) DO UPDATE
SET rename_policy = EXCLUDED.rename_policy,
    add_members_policy = EXCLUDED.add_members_policy,
    allow_self_leave = EXCLUDED.allow_self_leave,
    updated_at = NOW()
RETURNING channel_id, rename_policy, add_members_policy, allow_self_leave, updated_at;
