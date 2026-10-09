-- name: RegisterChannelTypingReaction :one
-- Commit the complete external anchor BEFORE issuing Add. A terminal observer
-- can enumerate every input, independently of the one delivery trigger.
INSERT INTO channel_typing_reaction (
 id, workspace_id, chat_session_id, chat_message_id, installation_id,
 channel_message_id, installation_snapshot, cleanup_required
)
SELECT @id, @workspace_id, @chat_session_id, @chat_message_id, @installation_id,
 @channel_message_id, @installation_snapshot, message.channel_typing_settled
FROM chat_message AS message
JOIN chat_session AS session ON session.id = message.chat_session_id
JOIN channel_installation AS installation ON installation.id = @installation_id
WHERE message.id = @chat_message_id
 AND session.id = @chat_session_id AND session.workspace_id = @workspace_id
 AND installation.workspace_id = @workspace_id AND installation.channel_type = 'feishu'
 AND message.channel_ingested AND message.role = 'user'
RETURNING *;

-- name: FinishChannelTypingReactionAdd :one
-- cleanup_required is monotonic. A stale active read cannot undo another
-- replica's terminal decision; late HTTP completion re-arms compensation.
UPDATE channel_typing_reaction SET reaction_id = @reaction_id, add_finished = true,
 cleanup_required = cleanup_required OR @cleanup_required::boolean,
 cleaned_at = NULL, retry_after = now()
WHERE id = @id
RETURNING *;

-- name: AcknowledgeChannelTypingReactionCleanup :exec
-- An unfinished Add may still create a reaction after an empty sweep. Retain
-- its anchor; only a known successful Add response can close cleanup forever.
UPDATE channel_typing_reaction
SET cleaned_at = CASE WHEN add_finished AND reaction_id <> '' THEN now() ELSE NULL END,
 retry_after = GREATEST(retry_after, now() + interval '30 seconds')
WHERE id = @id AND reaction_id = @reaction_id AND cleanup_required;

-- name: ClaimChannelTypingReactionCleanup :many
-- The DB is the shared source of eligibility. Missing source rows after a
-- committed deletion are terminal too. No transaction takes remote I/O locks.
WITH candidates AS (
 SELECT r.id FROM channel_typing_reaction r
 WHERE r.cleaned_at IS NULL AND r.retry_after <= now()
 AND (sqlc.narg('chat_session_id')::uuid IS NULL OR r.chat_session_id = sqlc.narg('chat_session_id'))
 AND (r.cleanup_required OR (NOT r.add_finished AND r.created_at < now() - interval '1 minute') OR NOT EXISTS (
  SELECT 1 FROM chat_message m
  JOIN chat_session s ON s.id = m.chat_session_id
  JOIN channel_installation i ON i.id = r.installation_id
  JOIN channel_chat_session_binding b ON b.chat_session_id = s.id AND b.installation_id = i.id
  LEFT JOIN agent_task_queue t ON t.id = m.task_id
  LEFT JOIN agent a ON a.id = t.agent_id
  WHERE m.id = r.chat_message_id AND s.id = r.chat_session_id
   AND s.workspace_id = r.workspace_id AND i.workspace_id = r.workspace_id
   AND i.channel_type = 'feishu' AND b.channel_type = 'feishu'
   AND s.status = 'active' AND i.status = 'active' AND b.retired_at IS NULL
   AND m.channel_ingested AND m.role = 'user' AND NOT m.channel_typing_settled
   AND (m.task_id IS NULL OR (t.chat_session_id = s.id AND a.workspace_id = r.workspace_id
     AND t.status IN ('queued','dispatched','running','waiting_local_directory','deferred')))
 ))
 ORDER BY r.retry_after, r.id LIMIT 100
 FOR UPDATE OF r SKIP LOCKED
)
UPDATE channel_typing_reaction r SET cleanup_required = true,
 attempts = attempts + 1,
 retry_after = now() + LEAST(3600, 30 * power(2, LEAST(r.attempts, 7))) * interval '1 second'
FROM candidates c WHERE r.id = c.id RETURNING r.*;

-- name: SettleChannelTypingInputs :exec
-- The flush owns only this context's unowned inputs up to its captured cutoff.
-- Later inputs, even in the same session, are not settled by this failed run.
UPDATE chat_message AS message SET channel_typing_settled = true
FROM chat_message AS cutoff, chat_session AS session, channel_installation AS installation
WHERE cutoff.id = @through_message_id AND cutoff.chat_session_id = @chat_session_id
 AND session.id = @chat_session_id AND session.workspace_id = @workspace_id
 AND installation.id = @installation_id AND installation.workspace_id = @workspace_id
 AND installation.channel_type = 'feishu'
 AND EXISTS (SELECT 1 FROM channel_chat_session_binding b WHERE b.chat_session_id = session.id
  AND b.installation_id = installation.id AND b.channel_type = 'feishu')
 AND message.chat_session_id = session.id AND message.task_id IS NULL
 AND message.channel_ingested AND message.role = 'user'
 AND COALESCE(message.channel_context_revision, 1) = @context_revision
 AND (message.created_at, message.id) <= (cutoff.created_at, cutoff.id);

-- name: PruneChannelTypingReactionCleanup :exec
-- Pending/failed/uncertain Adds are never discarded. Completed tombstones are
-- retained for seven days; all remote I/O has a much shorter bounded context.
DELETE FROM channel_typing_reaction WHERE cleaned_at < now() - interval '7 days';

-- name: SkipChannelTypingReactionAdd :exec
-- No HTTP Add was issued for an already-settled input.
UPDATE channel_typing_reaction SET add_finished = true, cleanup_required = true,
 cleaned_at = now() WHERE id = $1;

-- name: ListRequestedChannelTypingReactions :many
SELECT id FROM channel_typing_reaction WHERE id = ANY(@ids::uuid[]) AND cleanup_required;
