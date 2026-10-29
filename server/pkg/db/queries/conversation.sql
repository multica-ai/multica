-- Team conversations (MUL-8357): channels, member direct messages and their
-- threads. Access is decided in the handler (canAccessConversation); these
-- queries only scope by workspace.

-- name: CreateConversation :one
INSERT INTO conversation (workspace_id, kind, name, about, visibility, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CreateDirectConversation :one
-- ON CONFLICT returns no row when the pair already has one; the caller then
-- reads it with GetDirectConversation.
INSERT INTO conversation (workspace_id, kind, visibility, direct_key, created_by)
VALUES ($1, 'direct', 'private', $2, $3)
ON CONFLICT (workspace_id, direct_key) WHERE direct_key IS NOT NULL DO NOTHING
RETURNING *;

-- name: GetDirectConversation :one
SELECT * FROM conversation
WHERE workspace_id = $1 AND direct_key = $2;

-- name: CreateConversationThread :one
-- ON CONFLICT returns no row when the message already has a thread; the caller
-- then reads it with GetConversationThreadByRoot.
INSERT INTO conversation (workspace_id, kind, visibility, parent_conversation_id, root_message_id, created_by)
VALUES ($1, 'thread', $2, $3, $4, $5)
ON CONFLICT (root_message_id) WHERE root_message_id IS NOT NULL DO NOTHING
RETURNING *;

-- name: GetConversationThreadByRoot :one
SELECT * FROM conversation
WHERE root_message_id = $1;

-- name: GetConversationInWorkspace :one
SELECT * FROM conversation
WHERE id = $1 AND workspace_id = $2;

-- name: GetConversation :one
SELECT * FROM conversation
WHERE id = $1;

-- name: UpdateConversation :one
UPDATE conversation SET
    name = COALESCE(sqlc.narg('name'), name),
    about = COALESCE(sqlc.narg('about'), about),
    archived_at = CASE
        WHEN sqlc.narg('archived')::bool IS NULL THEN archived_at
        WHEN sqlc.narg('archived')::bool THEN COALESCE(archived_at, now())
        ELSE NULL
    END,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ListConversationsForMember :many
-- The channels and direct conversations a member sees: every public channel in
-- the workspace, plus every private channel and direct conversation they are
-- in. my_last_read_seq is 0 for a public channel they have not joined.
SELECT c.*,
       COALESCE(cm.last_read_seq, 0)::bigint AS my_last_read_seq,
       (cm.conversation_id IS NOT NULL)::bool AS is_member
FROM conversation c
LEFT JOIN conversation_member cm
  ON cm.conversation_id = c.id AND cm.member_type = 'member' AND cm.member_id = sqlc.arg(user_id)
WHERE c.workspace_id = sqlc.arg(workspace_id)
  AND c.kind IN ('channel', 'direct')
  AND (cm.conversation_id IS NOT NULL OR (c.kind = 'channel' AND c.visibility = 'public'))
ORDER BY c.kind, lower(c.name), c.created_at;

-- name: ListConversationMembersFor :many
SELECT * FROM conversation_member
WHERE conversation_id = ANY(sqlc.arg(conversation_ids)::uuid[])
ORDER BY conversation_id, created_at;

-- name: ListConversationMembers :many
SELECT * FROM conversation_member
WHERE conversation_id = $1
ORDER BY created_at;

-- name: GetConversationMember :one
SELECT * FROM conversation_member
WHERE conversation_id = $1 AND member_type = $2 AND member_id = $3;

-- name: AddConversationMember :exec
-- last_read_seq starts at the conversation's current end so a new member's
-- unread counts only what arrives after they join.
INSERT INTO conversation_member (conversation_id, workspace_id, member_type, member_id, last_read_seq)
VALUES ($1, $2, $3, $4, (SELECT last_message_seq FROM conversation WHERE id = $1))
ON CONFLICT (conversation_id, member_type, member_id) DO NOTHING;

-- name: RemoveConversationMember :exec
DELETE FROM conversation_member
WHERE conversation_id = $1 AND member_type = $2 AND member_id = $3;

-- name: MarkConversationRead :exec
UPDATE conversation_member
SET last_read_seq = GREATEST(last_read_seq, LEAST(sqlc.arg(seq)::bigint, (SELECT last_message_seq FROM conversation WHERE id = sqlc.arg(conversation_id))))
WHERE conversation_id = sqlc.arg(conversation_id) AND member_type = 'member' AND member_id = sqlc.arg(user_id);

-- name: NextConversationMessageSeq :one
-- Takes the conversation's row lock for the rest of the transaction, so seq is
-- gapless and messages commit in seq order.
UPDATE conversation
SET last_message_seq = last_message_seq + 1, last_message_at = now(), updated_at = now()
WHERE id = $1
RETURNING last_message_seq;

-- name: CreateConversationMessage :one
INSERT INTO conversation_message (conversation_id, workspace_id, seq, author_type, author_id, kind, content, task_id, client_message_id)
VALUES ($1, $2, $3, $4, sqlc.narg('author_id'), $5, $6, sqlc.narg('task_id'), sqlc.narg('client_message_id'))
RETURNING *;

-- name: GetConversationMessageByClientID :one
SELECT * FROM conversation_message
WHERE conversation_id = $1 AND author_id = $2 AND client_message_id = $3;

-- name: GetConversationMessage :one
SELECT * FROM conversation_message
WHERE id = $1;

-- name: ListConversationMessages :many
-- Newest first, before an optional seq cursor.
SELECT * FROM conversation_message
WHERE conversation_id = sqlc.arg(conversation_id)
  AND (sqlc.narg('before_seq')::bigint IS NULL OR seq < sqlc.narg('before_seq')::bigint)
ORDER BY seq DESC
LIMIT sqlc.arg(page_limit);

-- name: ListConversationThreadsByRoots :many
SELECT id, root_message_id, last_message_seq, last_message_at
FROM conversation
WHERE parent_conversation_id = sqlc.arg(parent_id)
  AND root_message_id = ANY(sqlc.arg(root_ids)::uuid[]);

-- name: ListConversationMessagesAfter :many
-- The context an agent is handed: messages after its last reply, newest first.
SELECT * FROM conversation_message
WHERE conversation_id = $1 AND seq > $2
ORDER BY seq DESC
LIMIT $3;

-- name: GetLastAgentReplySeq :one
SELECT COALESCE(MAX(seq), 0)::bigint FROM conversation_message
WHERE conversation_id = $1 AND author_type = 'agent' AND author_id = $2;

-- name: ListConversationTailAuthorTypes :many
-- The authors of the newest messages, for the agent-to-agent loop guard.
SELECT author_type FROM conversation_message
WHERE conversation_id = $1 AND kind = 'message'
ORDER BY seq DESC
LIMIT $2;

-- name: GetLatestAgentAuthor :one
-- The agent that replied most recently in a thread: it keeps the floor there
-- without a fresh @mention.
SELECT author_id FROM conversation_message
WHERE conversation_id = $1 AND author_type = 'agent' AND author_id IS NOT NULL
ORDER BY seq DESC
LIMIT 1;

-- name: GetLatestMemberAuthor :one
SELECT author_id FROM conversation_message
WHERE conversation_id = $1 AND author_type = 'member' AND author_id IS NOT NULL
ORDER BY seq DESC
LIMIT 1;

-- name: GetConversationAgentSession :one
SELECT * FROM conversation_agent_session
WHERE conversation_id = $1 AND agent_id = $2;

-- name: GetConversationAgentSessionByChat :one
SELECT * FROM conversation_agent_session
WHERE chat_session_id = $1;

-- name: UpsertConversationAgentSession :one
INSERT INTO conversation_agent_session (conversation_id, agent_id, workspace_id, chat_session_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (conversation_id, agent_id) DO UPDATE SET chat_session_id = EXCLUDED.chat_session_id
RETURNING *;

-- name: CreateConversationChatSession :one
-- The hidden session behind one agent's replies in one conversation.
INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id, conversation_id)
VALUES ($1, $2, $3, $4, (SELECT runtime_id FROM agent WHERE id = $2), $5)
RETURNING *;

-- name: ListConversationWorkingAgents :many
-- Agents with a reply in flight here, for the "is replying" row.
SELECT cas.agent_id, atq.id AS task_id
FROM conversation_agent_session cas
JOIN agent_task_queue atq ON atq.chat_session_id = cas.chat_session_id
WHERE cas.conversation_id = $1
  AND atq.chat_session_id IS NOT NULL
  AND atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
  AND atq.regenerate_quick_actions_for IS NULL
ORDER BY atq.created_at;
