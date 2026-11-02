-- Proposal lifecycle (spec 2.4): ask for changes, decide later, and expiry.
--
-- revision counts the body the boss reads; the leader's revision increments it.
-- expires_at is when a pending proposal with no decision becomes expired. It is
-- set on create and on revision, and a snooze moves it to a full window after
-- the snooze ends. snoozed_until hides a pending proposal from "Needs your
-- decision" until that time; the proposal still counts towards the pending caps.
--
-- The state CHECK is replaced: revising (changes requested, back with the
-- squad) and expired (no decision within the window, set by the daily job).
--
-- Pending proposals that predate this migration get a full window from the
-- deploy, so the first expiry run does not expire the whole backlog at once.
ALTER TABLE proposal ADD COLUMN IF NOT EXISTS revision INTEGER NOT NULL DEFAULT 1;
ALTER TABLE proposal ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
ALTER TABLE proposal ADD COLUMN IF NOT EXISTS snoozed_until TIMESTAMPTZ;

ALTER TABLE proposal DROP CONSTRAINT IF EXISTS proposal_state_check;
ALTER TABLE proposal ADD CONSTRAINT proposal_state_check
    CHECK (state IN ('pending', 'approved', 'rejected', 'revising', 'expired'));

UPDATE proposal SET expires_at = now() + interval '14 days'
WHERE state = 'pending' AND expires_at IS NULL;
