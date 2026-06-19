-- CEREBRO-PATCH(cerebro-reminder): FIR-394
-- Reminder as its own entity, decoupled from the message it was set on.
--
-- Before this migration a "remind me about this message" reminder lived as an
-- inbox_item row of type='reminder' whose issue_id pointed at the SOURCE
-- conversation (a DM or channel is an issue with kind='dm'/'channel'). That tied
-- reminder state to the conversation's identity, and a reminder row sitting on a
-- DM issue could lock the user out of opening that DM (FIR-249).
--
-- A reminder now stands on its own row and links BACK to the source message and
-- conversation, so deleting/refactoring reminders can never again break thread
-- loading. When a reminder fires, a fresh inbox notification is created that
-- links to the original message (see the reminder sweeper) — the reminder no
-- longer pretends to be an inbox item on the conversation.
CREATE TABLE IF NOT EXISTS cerebro_reminder (
    id                  UUID        DEFAULT gen_random_uuid() PRIMARY KEY,
    workspace_id        UUID        NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id             UUID        NOT NULL,
    remind_at           TIMESTAMPTZ NOT NULL,
    -- pending: scheduled, not yet fired. snoozed: rescheduled after firing.
    -- done: dismissed by the user. fired: surfaced in the inbox, awaiting action.
    status              TEXT        NOT NULL DEFAULT 'pending',
    -- Durable copy of the reminder text so the overview/card still has content
    -- even if the source message is later deleted.
    text                TEXT        NOT NULL DEFAULT '',
    -- Link back to the source. message_id is the comment the reminder was set on;
    -- conversation_id is the issue/DM/channel it lives in, so the "Go to message"
    -- action can navigate regardless of whether the source is a DM or a channel.
    -- ON DELETE SET NULL keeps the reminder as a safe dangling row instead of
    -- cascading it away when the source disappears.
    message_id          UUID        REFERENCES comment(id) ON DELETE SET NULL,
    conversation_id     UUID        REFERENCES issue(id) ON DELETE SET NULL,
    -- The inbox_item created when the reminder fired. NULL until fired; used to
    -- de-dupe re-fires and to find the surfaced row.
    fired_inbox_item_id UUID        REFERENCES inbox_item(id) ON DELETE SET NULL,
    fired_at            TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Sweeper claim path: pending reminders whose time has arrived.
CREATE INDEX IF NOT EXISTS idx_cerebro_reminder_due
    ON cerebro_reminder (remind_at)
    WHERE status = 'pending';

-- A member's reminder overview, newest scheduled first.
CREATE INDEX IF NOT EXISTS idx_cerebro_reminder_user
    ON cerebro_reminder (workspace_id, user_id, status, remind_at);

-- Data migration: move existing message-anchored reminders into the new entity
-- and remove the coupled inbox rows that caused the DM lockout. We only migrate
-- the comment-anchored, not-yet-fired reminders (the FIR-249 family): a non-NULL
-- details->>'comment_id', still muted in the future, not archived.
INSERT INTO cerebro_reminder
    (workspace_id, user_id, remind_at, status, text, message_id, conversation_id, created_at, updated_at)
SELECT
    ii.workspace_id,
    ii.recipient_id,
    ii.muted_until,
    'pending',
    COALESCE(ii.body, ''),
    NULLIF(ii.details->>'comment_id', '')::uuid,
    ii.issue_id,
    ii.created_at,
    NOW()
FROM inbox_item ii
WHERE ii.type = 'reminder'
  AND ii.recipient_type = 'member'
  AND ii.archived = false
  AND ii.muted_until IS NOT NULL
  AND ii.muted_until > NOW()
  AND COALESCE(ii.details->>'comment_id', '') <> '';

-- Drop the now-migrated source rows so they can no longer surface on, or couple
-- to, the conversation they pointed at. Reversible: the down migration recreates
-- equivalent inbox_item reminder rows from cerebro_reminder.
DELETE FROM inbox_item ii
WHERE ii.type = 'reminder'
  AND ii.recipient_type = 'member'
  AND ii.archived = false
  AND ii.muted_until IS NOT NULL
  AND ii.muted_until > NOW()
  AND COALESCE(ii.details->>'comment_id', '') <> '';
