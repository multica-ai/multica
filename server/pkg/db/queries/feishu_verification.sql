-- name: RecordFeishuInboundReceipt :exec
-- Rebind replaces config with a new generation and no receipt. Legacy configs
-- without a generation match only other legacy snapshots, never a new binding.
UPDATE channel_installation
SET config = jsonb_set(config, '{first_message_received_at}', to_jsonb(now()))
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND channel_type = 'feishu'
  AND status = 'active'
  AND config->>'app_id' = sqlc.arg('app_id')::text
  AND COALESCE(config->>'binding_generation', '') = sqlc.arg('binding_generation')::text
  AND config->>'first_message_received_at' IS NULL;

-- name: SetFeishuInstallationBotUnionID :exec
-- Preserve concurrently recorded receipt evidence and binding generation.
UPDATE channel_installation
SET config = jsonb_set(config, '{bot_union_id}', to_jsonb(sqlc.arg('bot_union_id')::text)),
    updated_at = now()
WHERE id = sqlc.arg('id') AND channel_type = 'feishu';

-- name: BackfillLegacyFeishuInboundReceipts :execrows
-- Session routes are positive receipt evidence only for the current binding.
-- Materialize the candidate timestamp; recheck eligibility after any concurrent
-- writer, preserving live receipt writes and never validating a later rebind.
WITH evidence AS MATERIALIZED (
    SELECT installation.id, installation.installed_at, min(binding.created_at) AS received_at
    FROM channel_installation AS installation
    JOIN channel_chat_session_binding AS binding
      ON binding.installation_id = installation.id
     AND binding.channel_type = installation.channel_type
     AND binding.created_at >= installation.installed_at
    WHERE installation.channel_type = 'feishu'
      AND installation.status = 'active'
      AND installation.config->>'binding_generation' IS NULL
      AND installation.config->>'first_message_received_at' IS NULL
    GROUP BY installation.id, installation.installed_at
    ORDER BY installation.id
    LIMIT sqlc.arg('batch_size')::int
)
UPDATE channel_installation AS installation
SET config = jsonb_set(installation.config, '{first_message_received_at}', to_jsonb(evidence.received_at))
FROM evidence
WHERE installation.id = evidence.id
  AND installation.installed_at = evidence.installed_at
  AND installation.channel_type = 'feishu'
  AND installation.status = 'active'
  AND installation.config->>'binding_generation' IS NULL
  AND installation.config->>'first_message_received_at' IS NULL;
