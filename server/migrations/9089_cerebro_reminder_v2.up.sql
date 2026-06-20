-- CEREBRO-PATCH(cerebro-reminder): FIR-394 v2 — the unified reminder.
--
-- v1 (9088) made a reminder its own entity but kept it narrow: a member
-- reminding THEMSELVES about a MESSAGE. Jesper's full scope: anyone can remind
-- any recipient — a person OR an agent — about anything: a DM/channel message,
-- an issue comment, an issue, a project, or nothing at all (just a description).
-- Agent recipients fire as agent wakeups (cerebro_agent_wakeup), so "remind an
-- agent" becomes a scheduled agent run, reusing the proven wakeup dispatcher.
--
-- This migration generalises the existing table in place. It only ever ran on
-- staging (v1 was never promoted to prod), so the backfill is trivial.

ALTER TABLE cerebro_reminder
    -- Who created the reminder. v1 had only user_id (= the self-owner); we split
    -- creator from recipient. For agent-recipient firing this becomes the
    -- wakeup's created_by_id, carrying the human origin a real agent run needs.
    ADD COLUMN IF NOT EXISTS creator_id     UUID,
    -- The recipient. 'member' lands in that person's inbox; 'agent' fires a
    -- wakeup. recipient_id replaces user_id as the "whose reminder is this"
    -- authority for the overview and authorization.
    ADD COLUMN IF NOT EXISTS recipient_type TEXT NOT NULL DEFAULT 'member',
    ADD COLUMN IF NOT EXISTS recipient_id   UUID,
    -- Polymorphic anchor — what the reminder points at:
    --   'comment' : message_id (a comment) + conversation_id (its DM/channel/issue)
    --   'issue'   : conversation_id holds the target issue (no message_id)
    --   'project' : project_id holds the target project
    --   'none'    : free reminder, just text
    -- DM vs channel vs issue is read from the conversation's kind, so we don't
    -- duplicate that distinction here.
    ADD COLUMN IF NOT EXISTS anchor_type    TEXT NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS project_id     UUID REFERENCES project(id) ON DELETE SET NULL;

-- Backfill the staging rows: v1 reminders were self-reminders on a message.
UPDATE cerebro_reminder
SET creator_id   = COALESCE(creator_id, user_id),
    recipient_id = COALESCE(recipient_id, user_id),
    anchor_type  = CASE WHEN message_id IS NOT NULL THEN 'comment' ELSE 'none' END
WHERE creator_id IS NULL OR recipient_id IS NULL;

ALTER TABLE cerebro_reminder
    ALTER COLUMN creator_id   SET NOT NULL,
    ALTER COLUMN recipient_id SET NOT NULL;

ALTER TABLE cerebro_reminder
    DROP CONSTRAINT IF EXISTS cerebro_reminder_recipient_type_check,
    ADD  CONSTRAINT cerebro_reminder_recipient_type_check
         CHECK (recipient_type IN ('member', 'agent')),
    DROP CONSTRAINT IF EXISTS cerebro_reminder_anchor_type_check,
    ADD  CONSTRAINT cerebro_reminder_anchor_type_check
         CHECK (anchor_type IN ('comment', 'issue', 'project', 'none'));

-- The overview now keys on recipient, not the old self-owner user_id.
DROP INDEX IF EXISTS idx_cerebro_reminder_user;
CREATE INDEX IF NOT EXISTS idx_cerebro_reminder_recipient
    ON cerebro_reminder (workspace_id, recipient_type, recipient_id, status, remind_at);
