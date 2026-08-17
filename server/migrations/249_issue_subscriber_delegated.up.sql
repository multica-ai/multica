-- MUL-5483: an agent creating an issue on a human's behalf must carry that
-- human's visibility with it.
--
-- Two changes, both required by the same rule ("delegation preserves
-- accountability, so it must preserve visibility"):
--
-- 1. reason='delegated' — a new provenance label for "your agent created this
--    on your behalf". It is deliberately NOT folded into 'creator': the DB
--    creator really is the agent, and the notification tier for a delegated
--    subscription is narrower than a direct one (see notification_listeners.go
--    delegatedNotifTypes). A distinct reason is what lets the UI explain the
--    subscription and lets delivery treat it differently.
--
-- 2. unsubscribed_at — an explicit opt-out tombstone. Until now unsubscribing
--    was row ABSENCE, which was survivable only because auto-subscribe rules
--    fired rarely. The delegated rule fires on every agent-created issue, so a
--    deleted row would be silently re-created by the next rule pass and
--    unsubscribe would not stick. Auto-subscribe uses ON CONFLICT DO NOTHING,
--    so a tombstoned row is never resurrected by a rule; only an explicit user
--    subscribe clears it.
-- The CHECK is only WIDENED (one new allowed value), so every existing row
-- already satisfies it. A plain ADD CONSTRAINT would still hold ACCESS
-- EXCLUSIVE on issue_subscriber for a full scan, blocking subscriber reads and
-- writes on a hot table for the duration. Use the two-step NOT VALID +
-- VALIDATE CONSTRAINT pattern from migration 060: the add takes the strong
-- lock only briefly (no scan), and VALIDATE does the scan under SHARE UPDATE
-- EXCLUSIVE, which does not block concurrent reads or writes. The constraint
-- still ends up fully validated, so no un-trusted constraint is left behind.
ALTER TABLE issue_subscriber DROP CONSTRAINT IF EXISTS issue_subscriber_reason_check;
ALTER TABLE issue_subscriber ADD CONSTRAINT issue_subscriber_reason_check
    CHECK (reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual', 'autopilot', 'delegated')) NOT VALID;
ALTER TABLE issue_subscriber VALIDATE CONSTRAINT issue_subscriber_reason_check;

-- Nullable, no default, so this is a catalog-only change in PostgreSQL 11+
-- and does not rewrite the table.
ALTER TABLE issue_subscriber ADD COLUMN unsubscribed_at TIMESTAMPTZ;
