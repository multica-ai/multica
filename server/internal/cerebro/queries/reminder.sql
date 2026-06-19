-- CEREBRO-PATCH(cerebro-reminder): FIR-394 — reminder as its own entity, linked
-- back to the source message + conversation. Decouples reminder state from the
-- DM/channel thread so a reminder can never lock a conversation (FIR-249).

-- name: ListReminders :many
-- A member's reminders for the overview, joined with the source message preview
-- and the conversation it belongs to. Source columns are nullable: the reminder
-- survives (ON DELETE SET NULL) even if the message/conversation was deleted.
SELECT
    r.id, r.workspace_id, r.user_id, r.remind_at, r.status, r.text,
    r.message_id, r.conversation_id, r.fired_inbox_item_id, r.fired_at,
    r.created_at, r.updated_at,
    c.content      AS source_content,
    c.author_type  AS source_author_type,
    c.author_id    AS source_author_id,
    i.kind         AS conversation_kind,
    i.title        AS conversation_title
FROM cerebro_reminder r
LEFT JOIN comment c ON c.id = r.message_id
LEFT JOIN issue   i ON i.id = r.conversation_id
WHERE r.workspace_id = $1 AND r.user_id = $2 AND r.status <> 'done'
ORDER BY r.remind_at ASC;

-- name: GetReminder :one
-- Single reminder with the same source join, used for the opened reminder card
-- and as the authorization load (returns user_id + workspace_id).
SELECT
    r.id, r.workspace_id, r.user_id, r.remind_at, r.status, r.text,
    r.message_id, r.conversation_id, r.fired_inbox_item_id, r.fired_at,
    r.created_at, r.updated_at,
    c.content      AS source_content,
    c.author_type  AS source_author_type,
    c.author_id    AS source_author_id,
    i.kind         AS conversation_kind,
    i.title        AS conversation_title
FROM cerebro_reminder r
LEFT JOIN comment c ON c.id = r.message_id
LEFT JOIN issue   i ON i.id = r.conversation_id
WHERE r.id = $1;

-- name: CreateReminder :one
-- Create a reminder anchored to a message in a conversation. Returns the id;
-- the handler re-reads via GetReminder to build the joined response.
INSERT INTO cerebro_reminder (workspace_id, user_id, remind_at, text, message_id, conversation_id)
VALUES ($1, $2, $3, $4, $5, $6)
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
-- double-claiming. Returns the rows the sweeper needs to re-surface the source
-- conversation in the inbox.
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
RETURNING id, workspace_id, user_id, remind_at, text, message_id, conversation_id;

-- name: SetReminderFiredInboxItem :exec
-- Record which inbox row the sweeper surfaced/created for a fired reminder, so a
-- later re-fire can dedupe against it.
UPDATE cerebro_reminder
SET fired_inbox_item_id = $2,
    updated_at          = NOW()
WHERE id = $1;
