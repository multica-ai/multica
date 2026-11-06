-- name: GetMeta :one
SELECT value FROM store_meta WHERE key = ?;

-- name: PutMeta :exec
INSERT INTO store_meta (key, value) VALUES (?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value;

-- name: GetScopeID :one
SELECT id FROM scope WHERE api_origin = ? AND user_id = ? AND workspace_id = ?;

-- name: InsertScope :one
INSERT INTO scope (api_origin, user_id, workspace_id) VALUES (?, ?, ?) RETURNING id;

-- name: BumpScopeSeq :one
UPDATE scope SET scope_seq = scope_seq + 1 WHERE id = ? RETURNING scope_seq;

-- name: GetScopeSeq :one
SELECT scope_seq FROM scope WHERE id = ?;

-- name: InsertConversation :exec
INSERT INTO conversation (
    id, scope_id, provider, provider_session_id, title, title_mode, cwd, model,
    reasoning_effort, permission_mode, project_id, side_source_id, side, forked_from_id,
    forked_from_turn_id, imported_from_session_id, summary_seq, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetConversation :one
SELECT * FROM conversation WHERE id = ?;

-- Select the last user turn through turn_order before reading its messages.
-- A history-wide join would sort every message on each summary update.
-- name: GetConversationSummary :one
SELECT sqlc.embed(conversation), CAST(COALESCE((
    SELECT m.created_at FROM message m
    WHERE m.conversation_id = conversation.id AND m.role = 'user' AND m.steered = 0
      AND m.turn_id = (
          SELECT t.id FROM turn t WHERE t.conversation_id = conversation.id
            AND EXISTS (SELECT 1 FROM message input WHERE input.conversation_id = t.conversation_id
                        AND input.turn_id = t.id AND input.role = 'user' AND input.steered = 0)
          ORDER BY t.turn_index DESC LIMIT 1
      )
    ORDER BY m.created_at, m.id LIMIT 1
), '') AS TEXT) AS last_turn_sent_at FROM conversation WHERE conversation.id = ?;

-- name: AdvanceConversationSeq :exec
UPDATE conversation SET last_seq = ? WHERE id = ?;

-- name: UpdateConversationSummary :exec
UPDATE conversation SET
    status = ?,
    title = ?,
    last_message_preview = ?,
    pending_interactions = ?,
    queue_count = ?,
    background_tasks = ?,
    summary_seq = ?,
    updated_at = ?
WHERE id = ?;

-- name: SetProviderSessionID :exec
UPDATE conversation SET provider_session_id = ? WHERE id = ?;

-- name: SetConversationTurnCount :exec
UPDATE conversation SET turn_count = ? WHERE id = ?;

-- name: ListSummariesAfter :many
SELECT sqlc.embed(conversation), CAST(COALESCE((
    SELECT m.created_at FROM message m
    WHERE m.conversation_id = conversation.id AND m.role = 'user' AND m.steered = 0
      AND m.turn_id = (
          SELECT t.id FROM turn t WHERE t.conversation_id = conversation.id
            AND EXISTS (SELECT 1 FROM message input WHERE input.conversation_id = t.conversation_id
                        AND input.turn_id = t.id AND input.role = 'user' AND input.steered = 0)
          ORDER BY t.turn_index DESC LIMIT 1
      )
    ORDER BY m.created_at, m.id LIMIT 1
), '') AS TEXT) AS last_turn_sent_at FROM conversation
WHERE scope_id = sqlc.arg(scope_id) AND summary_seq > sqlc.arg(after_scope_seq)
ORDER BY summary_seq
LIMIT sqlc.arg(row_limit);

-- name: InsertTurn :exec
INSERT INTO turn (id, conversation_id, turn_index, request_id, remote, origin_peer_id, origin_client_kind, status, started_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTurn :one
SELECT * FROM turn WHERE id = ?;

-- name: AdvanceTurnSeq :exec
UPDATE turn SET last_turn_seq = ? WHERE id = ?;

-- name: CompleteTurn :exec
UPDATE turn SET status = ?, error_kind = ?, error = ?, completed_at = ? WHERE id = ?;

-- name: SetProviderTurnID :exec
UPDATE turn SET provider_turn_id = ? WHERE id = ?;

-- name: ListRecentTurns :many
SELECT * FROM turn
WHERE conversation_id = sqlc.arg(conversation_id)
ORDER BY turn_index DESC
LIMIT sqlc.arg(row_limit);

-- name: ListTurnsBefore :many
SELECT * FROM turn
WHERE conversation_id = sqlc.arg(conversation_id) AND turn_index < sqlc.arg(before_index)
ORDER BY turn_index DESC
LIMIT sqlc.arg(row_limit);

-- name: ListRunningTurns :many
SELECT * FROM turn WHERE status = 'running' ORDER BY conversation_id, turn_index;

-- name: InsertMessage :exec
INSERT INTO message (id, conversation_id, turn_id, role, content, attachment_ids, steered, failed, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListTurnMessages :many
SELECT * FROM message
WHERE conversation_id = ? AND turn_id = ?
ORDER BY created_at, id;

-- name: InsertEvent :exec
INSERT INTO event (conversation_id, seq, turn_id, turn_seq, item_id, type, final, payload, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListEventsAfter :many
SELECT * FROM event
WHERE conversation_id = sqlc.arg(conversation_id)
  AND seq > sqlc.arg(after_seq)
  AND seq <= sqlc.arg(through_seq)
ORDER BY seq
LIMIT sqlc.arg(row_limit);

-- name: ListTurnEvents :many
SELECT * FROM event
WHERE conversation_id = ? AND turn_id = ?
ORDER BY seq;

-- name: ListTurnFinalEvents :many
SELECT * FROM event
WHERE conversation_id = ? AND turn_id = ? AND final = 1
ORDER BY seq;

-- name: ListItemEvents :many
SELECT * FROM event
WHERE conversation_id = ? AND item_id = ?
ORDER BY seq;

-- name: ListPendingInteractions :many
SELECT * FROM interaction_request
WHERE conversation_id = ? AND status = 'pending'
ORDER BY created_at, id;

-- name: InsertAttachment :exec
INSERT INTO attachment (id, scope_id, conversation_id, path, file_name, size, media_type, sha256, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetAttachment :one
SELECT * FROM attachment WHERE id = ?;

-- name: CountAttachmentsAtPath :one
SELECT COUNT(*) FROM attachment WHERE path = ?;

-- name: FindUploadByHash :one
SELECT * FROM attachment
WHERE scope_id = ? AND sha256 = ? AND sha256 != '' AND conversation_id = ''
ORDER BY created_at DESC
LIMIT 1;

-- name: DeleteAttachment :exec
DELETE FROM attachment WHERE id = ?;

-- name: ListUploadsCreatedBefore :many
SELECT * FROM attachment WHERE conversation_id = '' AND created_at < ?;

-- name: InsertCommandReceipt :exec
INSERT INTO command_receipt (scope_id, request_id, fingerprint, result, uncertain, created_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetCommandReceipt :one
SELECT * FROM command_receipt WHERE scope_id = ? AND request_id = ?;

-- name: MarkCommandUncertain :exec
UPDATE command_receipt SET uncertain = 1 WHERE scope_id = ? AND request_id = ?;

-- name: UpdateConversationSettings :exec
UPDATE conversation SET model = ?, reasoning_effort = ?, permission_mode = ? WHERE id = ?;

-- name: ListScopeConversations :many
SELECT * FROM conversation WHERE scope_id = ? ORDER BY updated_at DESC, id;

-- name: ListConversationMessages :many
SELECT * FROM message WHERE conversation_id = ? ORDER BY created_at, id;

-- name: GetScope :one
SELECT * FROM scope WHERE id = ?;

-- name: InsertInteraction :exec
INSERT INTO interaction_request (id, conversation_id, turn_id, provider_request_id, kind, payload, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: ResolveInteraction :execrows
UPDATE interaction_request
SET status = ?, answer = ?, answered_by_connection = ?, resolved_at = ?
WHERE id = ? AND status = 'pending';

-- name: GetInteraction :one
SELECT * FROM interaction_request WHERE id = ?;

-- name: CountPendingInteractions :one
SELECT COUNT(*) FROM interaction_request WHERE conversation_id = ? AND status = 'pending';

-- name: ListPendingTurnInteractions :many
SELECT * FROM interaction_request WHERE conversation_id = ? AND turn_id = ? AND status = 'pending' ORDER BY created_at, id;

-- name: UpdateConversationMetadata :exec
UPDATE conversation SET title_mode = ?, title_generation = ?, cwd = ?, project_id = ?, pinned = ?, archived = ? WHERE id = ?;

-- name: InsertFolderProject :exec
INSERT INTO folder_project (id, scope_id, title, path, created_at) VALUES (?, ?, ?, ?, ?);

-- name: ListFolderProjects :many
SELECT * FROM folder_project WHERE scope_id = ? ORDER BY created_at, id;

-- name: GetFolderProjectByPath :one
SELECT * FROM folder_project WHERE scope_id = ? AND path = ?;

-- name: GetFolderProject :one
SELECT * FROM folder_project WHERE scope_id = ? AND id = ?;

-- name: FindConversationByCwd :one
SELECT * FROM conversation WHERE scope_id = ? AND cwd = ? ORDER BY created_at LIMIT 1;

-- name: InsertPreviewRoot :exec
INSERT INTO preview_root (scope_id, path, created_at) VALUES (?, ?, ?)
ON CONFLICT (scope_id, path) DO NOTHING;

-- name: ListPreviewRoots :many
SELECT path FROM preview_root WHERE scope_id = ? ORDER BY created_at, path;

-- name: CountOpenBackgroundTasks :one
SELECT COUNT(DISTINCT e.item_id) FROM event e
WHERE e.conversation_id = ? AND e.item_id IS NOT NULL AND json_extract(e.payload, '$.kind') = 'background_task'
  AND NOT EXISTS (SELECT 1 FROM event f WHERE f.conversation_id = e.conversation_id AND f.item_id = e.item_id AND f.final = 1);

-- name: ListConversationsWithBackgroundTasks :many
SELECT id FROM conversation WHERE background_tasks > 0 ORDER BY id;

-- name: ListBackgroundTaskEvents :many
SELECT * FROM event
WHERE conversation_id = ? AND item_id IS NOT NULL AND json_extract(payload, '$.kind') = 'background_task'
ORDER BY seq;

-- name: GetPromptQueue :one
SELECT * FROM prompt_queue WHERE conversation_id = ?;

-- name: UpsertPromptQueue :exec
INSERT INTO prompt_queue (conversation_id, revision, paused, error) VALUES (?, ?, ?, ?)
ON CONFLICT (conversation_id) DO UPDATE SET revision = excluded.revision, paused = excluded.paused, error = excluded.error;

-- name: ListPromptQueueItems :many
SELECT * FROM prompt_queue_item WHERE conversation_id = ? ORDER BY position, request_id;

-- name: InsertPromptQueueItem :exec
INSERT INTO prompt_queue_item (conversation_id, request_id, position, item, origin_peer_id, origin_client_kind) VALUES (?, ?, ?, ?, ?, ?);

-- name: DeletePromptQueueItem :exec
DELETE FROM prompt_queue_item WHERE conversation_id = ? AND request_id = ?;

-- name: SetPromptQueueItemPosition :exec
UPDATE prompt_queue_item SET position = ? WHERE conversation_id = ? AND request_id = ?;

-- name: PromptQueueBounds :one
SELECT CAST(COALESCE(MIN(position), 0) AS INTEGER) AS first_position, CAST(COALESCE(MAX(position), 0) AS INTEGER) AS last_position, COUNT(*) AS item_count
FROM prompt_queue_item WHERE conversation_id = ?;

-- name: ListQueuedConversations :many
SELECT DISTINCT conversation_id FROM prompt_queue_item ORDER BY conversation_id;

-- name: SetCommandReceiptResult :exec
UPDATE command_receipt SET result = ? WHERE scope_id = ? AND request_id = ?;

-- name: FindImportedConversation :one
SELECT * FROM conversation
WHERE scope_id = ? AND provider = ? AND (provider_session_id = sqlc.arg(session_id) OR imported_from_session_id = sqlc.arg(session_id))
LIMIT 1;

-- name: SetTurnRemote :exec
UPDATE turn SET remote = ? WHERE id = ?;

-- name: ListConversationTurns :many
SELECT * FROM turn WHERE conversation_id = ? ORDER BY turn_index;

-- name: CountSessionBrowseGroups :many
SELECT CAST(COALESCE(f.id, '') AS TEXT) AS group_id, COUNT(*) AS total
FROM conversation AS c
LEFT JOIN folder_project AS f ON f.scope_id = c.scope_id
  AND rtrim(f.path, '/\') = rtrim(c.cwd, '/\')
  AND (rtrim(f.path, '/\') <> '' OR f.path = c.cwd)
WHERE c.scope_id = sqlc.arg(scope_id) AND c.archived = sqlc.arg(archived)
  AND (sqlc.arg(search) = ''
    OR instr(lower(c.title), lower(sqlc.arg(search))) > 0
    OR instr(lower(c.last_message_preview), lower(sqlc.arg(search))) > 0
    OR instr(lower(COALESCE(f.title, '')), lower(sqlc.arg(search))) > 0
    OR instr(lower(COALESCE(f.path, '')), lower(sqlc.arg(search))) > 0)
GROUP BY CAST(COALESCE(f.id, '') AS TEXT);

-- name: BrowseSessionPage :many
WITH recency AS (
    SELECT c.id, c.created_at, CAST(COALESCE((
    SELECT m.created_at FROM message m
    WHERE m.conversation_id = c.id AND m.role = 'user' AND m.steered = 0
      AND m.turn_id = (
          SELECT t.id FROM turn t WHERE t.conversation_id = c.id
            AND EXISTS (SELECT 1 FROM message input WHERE input.conversation_id = t.conversation_id
                        AND input.turn_id = t.id AND input.role = 'user' AND input.steered = 0)
          ORDER BY t.turn_index DESC LIMIT 1
      )
    ORDER BY m.created_at, m.id LIMIT 1
), '') AS TEXT) AS last_turn_sent_at FROM conversation AS c
    WHERE c.scope_id = sqlc.arg(scope_id)
), sort_keys AS (
    -- Stored instants are UTC RFC3339Nano. Fixed-width fractions preserve
    -- nanoseconds in TEXT comparisons, including whole-second timestamps.
    SELECT id, last_turn_sent_at,
        substr(COALESCE(NULLIF(last_turn_sent_at, ''), created_at), 1, 19) || '.' ||
        substr(rtrim(replace(substr(COALESCE(NULLIF(last_turn_sent_at, ''), created_at), 20), '.', ''), 'Z') || '000000000', 1, 9) || 'Z' AS sort_at
    FROM recency
)
SELECT sqlc.embed(c), CAST(r.last_turn_sent_at AS TEXT) AS last_turn_sent_at
FROM conversation AS c
JOIN sort_keys AS r ON r.id = c.id
LEFT JOIN folder_project AS f ON f.scope_id = c.scope_id
  AND rtrim(f.path, '/\') = rtrim(c.cwd, '/\')
  AND (rtrim(f.path, '/\') <> '' OR f.path = c.cwd)
WHERE c.scope_id = sqlc.arg(scope_id) AND c.archived = sqlc.arg(archived)
  AND (sqlc.arg(filter_group) = 0 OR COALESCE(f.id, '') = sqlc.arg(group_id))
  AND c.id <> sqlc.arg(exclude_id)
  AND (sqlc.arg(source_id) = '' OR c.side_source_id = sqlc.arg(source_id))
  AND (sqlc.arg(search) = ''
    OR instr(lower(c.title), lower(sqlc.arg(search))) > 0
    OR instr(lower(c.last_message_preview), lower(sqlc.arg(search))) > 0
    OR instr(lower(COALESCE(f.title, '')), lower(sqlc.arg(search))) > 0
    OR instr(lower(COALESCE(f.path, '')), lower(sqlc.arg(search))) > 0)
  AND (sqlc.arg(cursor_id) = '' OR c.pinned < sqlc.arg(cursor_pinned)
    OR (c.pinned = sqlc.arg(cursor_pinned) AND r.sort_at < CAST(sqlc.arg(cursor_sort_at) AS TEXT))
    OR (c.pinned = sqlc.arg(cursor_pinned) AND r.sort_at = CAST(sqlc.arg(cursor_sort_at) AS TEXT) AND c.id > sqlc.arg(cursor_id)))
ORDER BY c.pinned DESC, r.sort_at DESC, c.id
LIMIT sqlc.arg(row_limit);

-- name: ListTurnItemStarts :many
SELECT item_id, MIN(seq) AS first_seq, created_at
FROM event
WHERE conversation_id = sqlc.arg(conversation_id) AND turn_id = sqlc.arg(turn_id) AND item_id IS NOT NULL
GROUP BY item_id;

-- name: InsertPromptQueueTombstone :exec
INSERT OR REPLACE INTO prompt_queue_tombstone (conversation_id, request_id, removed_at) VALUES (?, ?, ?);

-- name: DeleteOldPromptQueueTombstones :exec
DELETE FROM prompt_queue_tombstone WHERE conversation_id = ? AND removed_at < ?;

-- name: CountPromptQueueTombstone :one
SELECT COUNT(*) FROM prompt_queue_tombstone WHERE conversation_id = ? AND request_id = ?;

-- name: GetSubmission :one
SELECT * FROM prompt_submission WHERE conversation_id = ? AND request_id = ?;

-- name: ListOpenSubmissions :many
SELECT * FROM prompt_submission WHERE prompt_submission.conversation_id = ? AND (state IN ('queued', 'starting', 'recovery_required') OR request_id IN (SELECT conversation_execution.request_id FROM conversation_execution WHERE conversation_execution.conversation_id = prompt_submission.conversation_id)) ORDER BY updated_at, request_id;

-- name: PutSubmission :exec
INSERT INTO prompt_submission (conversation_id, request_id, item, state, revision, attempt_id, message_id, blocked_reason, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (conversation_id, request_id) DO UPDATE SET item = CASE WHEN excluded.state = 'removed' THEN '{}' ELSE prompt_submission.item END, state = excluded.state, revision = excluded.revision, attempt_id = excluded.attempt_id, message_id = excluded.message_id, blocked_reason = excluded.blocked_reason, updated_at = excluded.updated_at;

-- name: GetAttempt :one
SELECT * FROM prompt_attempt WHERE conversation_id = ? AND attempt_id = ?;

-- name: GetAttemptByRequest :one
SELECT * FROM prompt_attempt WHERE conversation_id = ? AND request_id = ? ORDER BY rowid DESC LIMIT 1;

-- name: PutAttempt :exec
INSERT INTO prompt_attempt (conversation_id, request_id, attempt_id, submission_id, delivery, turn_ids, settings)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (conversation_id, attempt_id) DO UPDATE SET delivery = excluded.delivery, turn_ids = excluded.turn_ids, settings = excluded.settings;

-- name: GetExecution :one
SELECT * FROM conversation_execution WHERE conversation_id = ?;

-- name: ClaimExecution :exec
INSERT INTO conversation_execution (conversation_id, request_id, attempt_id) VALUES (?, ?, ?);

-- name: ReleaseExecution :exec
DELETE FROM conversation_execution WHERE conversation_id = ? AND attempt_id = ?;

-- name: ListExecutions :many
SELECT * FROM conversation_execution;

-- name: GetQueueItemPosition :one
SELECT position FROM prompt_queue_item WHERE conversation_id = ? AND request_id = ?;

-- name: ListAttempts :many
SELECT * FROM prompt_attempt WHERE conversation_id = ? AND submission_id = ?;

-- name: DeleteConversationSubmissions :exec
DELETE FROM prompt_submission WHERE conversation_id = ?;

-- name: DeleteConversationAttempts :exec
DELETE FROM prompt_attempt WHERE conversation_id = ?;

-- name: DeleteConversationExecution :exec
DELETE FROM conversation_execution WHERE conversation_id = ?;

-- name: GetQueueOperation :one
SELECT * FROM queue_operation WHERE conversation_id = ? AND operation_id = ?;

-- name: InsertQueueOperation :exec
INSERT INTO queue_operation (conversation_id, operation_id, action, request_id) VALUES (?, ?, ?, ?);

-- name: DeleteConversationQueueOperations :exec
DELETE FROM queue_operation WHERE conversation_id = ?;
