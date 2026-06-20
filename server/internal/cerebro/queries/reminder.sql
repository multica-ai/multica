-- CEREBRO-PATCH(cerebro-reminder): FIR-394 — the unified reminder. A reminder is
-- "remind [a member OR an agent] about [a comment / issue / project / nothing]
-- at [a time]". It links BACK to its anchor (ON DELETE SET NULL) so it never
-- couples to thread loading (FIR-249), and agent recipients fire as wakeups.

-- name: ListReminders :many
-- A recipient's reminders for the overview, joined with whatever the reminder is
-- anchored to (message preview + conversation, or project). All anchor columns
-- are nullable: the reminder survives even if its anchor was deleted.
SELECT
    r.id, r.workspace_id, r.creator_id, r.recipient_type, r.recipient_id,
    r.remind_at, r.status, r.text, r.anchor_type,
    r.message_id, r.conversation_id, r.project_id, r.chat_message_id,
    r.fired_inbox_item_id, r.fired_at, r.created_at, r.updated_at,
    c.content      AS source_content,
    c.author_type  AS source_author_type,
    c.author_id    AS source_author_id,
    i.kind         AS conversation_kind,
    i.title        AS conversation_title,
    p.title        AS project_title,
    cm.content     AS chat_source_content,
    cs.id          AS chat_session_id,
    cs.title       AS chat_session_title
FROM cerebro_reminder r
LEFT JOIN comment      c  ON c.id  = r.message_id
LEFT JOIN issue        i  ON i.id  = r.conversation_id
LEFT JOIN project      p  ON p.id  = r.project_id
LEFT JOIN chat_message cm ON cm.id = r.chat_message_id
LEFT JOIN chat_session cs ON cs.id = cm.chat_session_id
WHERE r.workspace_id = $1
  AND r.recipient_type = $2
  AND r.recipient_id = $3
  AND r.status <> 'done'
ORDER BY r.remind_at ASC;

-- name: GetReminder :one
-- Single reminder with the same anchor joins, used for the opened reminder card
-- and as the authorization load (returns recipient_type + recipient_id).
SELECT
    r.id, r.workspace_id, r.creator_id, r.recipient_type, r.recipient_id,
    r.remind_at, r.status, r.text, r.anchor_type,
    r.message_id, r.conversation_id, r.project_id, r.chat_message_id,
    r.fired_inbox_item_id, r.fired_at, r.created_at, r.updated_at,
    c.content      AS source_content,
    c.author_type  AS source_author_type,
    c.author_id    AS source_author_id,
    i.kind         AS conversation_kind,
    i.title        AS conversation_title,
    p.title        AS project_title,
    cm.content     AS chat_source_content,
    cs.id          AS chat_session_id,
    cs.title       AS chat_session_title
FROM cerebro_reminder r
LEFT JOIN comment      c  ON c.id  = r.message_id
LEFT JOIN issue        i  ON i.id  = r.conversation_id
LEFT JOIN project      p  ON p.id  = r.project_id
LEFT JOIN chat_message cm ON cm.id = r.chat_message_id
LEFT JOIN chat_session cs ON cs.id = cm.chat_session_id
WHERE r.id = $1;

-- name: CreateReminder :one
-- Create a reminder for any recipient, anchored to any (or no) entity. user_id
-- is kept populated (= creator) for back-compat with the v1 column; recipient_id
-- is the authority. Returns the id; the handler re-reads via GetReminder.
INSERT INTO cerebro_reminder (
    workspace_id, user_id, creator_id, recipient_type, recipient_id,
    remind_at, text, anchor_type, message_id, conversation_id, project_id,
    chat_message_id
)
VALUES ($1, $2, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id;

-- name: SnoozeReminder :one
-- Reschedule a reminder (the "Udsæt" action). Returns it to pending and clears
-- the fired markers so the sweeper picks it up again.
UPDATE cerebro_reminder
SET remind_at           = $2,
    status              = 'pending',
    fired_at            = NULL,
    fired_inbox_item_id = NULL,
    updated_at          = NOW()
WHERE id = $1
RETURNING id;

-- name: MarkReminderDone :one
-- Dismiss a reminder (the "Færdig" action).
UPDATE cerebro_reminder
SET status     = 'done',
    updated_at = NOW()
WHERE id = $1
RETURNING id;

-- name: DeleteReminder :exec
DELETE FROM cerebro_reminder WHERE id = $1;

-- name: ClaimDueReminders :many
-- Atomically claim pending reminders whose time has arrived by flipping them to
-- 'fired'. FOR UPDATE SKIP LOCKED keeps concurrent sweeper ticks from
-- double-claiming. Returns everything the sweeper needs to fan out: a member
-- recipient re-surfaces the source in the inbox; an agent recipient fires a
-- wakeup (needs creator_id for human origin + the anchor/issue context).
UPDATE cerebro_reminder
SET status     = 'fired',
    fired_at   = NOW(),
    updated_at = NOW()
WHERE id IN (
    SELECT id FROM cerebro_reminder
    WHERE status = 'pending' AND remind_at <= NOW()
    ORDER BY remind_at ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, workspace_id, creator_id, recipient_type, recipient_id,
          remind_at, text, anchor_type, message_id, conversation_id, project_id,
          chat_message_id;

-- name: SetReminderFiredInboxItem :exec
-- Record which inbox row the sweeper surfaced/created for a fired reminder, so a
-- later re-fire can dedupe against it.
UPDATE cerebro_reminder
SET fired_inbox_item_id = $2,
    updated_at          = NOW()
WHERE id = $1;

-- name: GetChatMessageWithWorkspace :one
-- Resolve + authorize a chat message for the chat-message reminder anchor: the
-- row only comes back if it belongs to the given workspace (via its session).
-- Returns the message's session so the reminder can re-open the right chat.
SELECT cm.id, cm.chat_session_id, cm.content, cs.workspace_id, cs.title AS chat_title
FROM chat_message cm
JOIN chat_session cs ON cs.id = cm.chat_session_id
WHERE cm.id = $1 AND cs.workspace_id = $2;

-- name: MarkChatSessionUnreadByMessage :exec
-- Re-surface a chat in the inbox when a chat-message reminder fires: stamp the
-- session unread (idempotent — no-op if it is already unread) by the message id,
-- so the sweeper needs only the chat_message_id it already carries.
UPDATE chat_session
SET unread_since = NOW()
WHERE id = (SELECT cm.chat_session_id FROM chat_message cm WHERE cm.id = $1)
  AND unread_since IS NULL;
