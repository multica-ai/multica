-- TECH-3298: wakeup notes + self-wakeup limits.
--
-- Two cerebro-side changes, both idempotent (TestMigrationsAreIdempotent):
--
-- 1. A new comment type 'wakeup'. The wakeup dispatcher used to write a plain
--    'comment' row ("**Wakeup:** ...") that rendered as a full comment card and
--    flooded the issue thread. We now tag those rows type='wakeup' so the
--    frontend can render them as a small, collapsible action note (like a
--    status change) instead of a full comment. The trigger sub-type + the
--    agent's note are carried inline in the comment content.
--
-- 2. Two per-workspace self-wakeup limits on cerebro_workspace_settings:
--    how many times an agent may wake itself on the same issue, and the
--    minimum gap between two time-based wakeups. Defaults match the values
--    agreed on TECH-3298 (8 wakeups / 5 minutes). A missing row -> defaults.

-- 1. Extend the comment type CHECK to allow 'wakeup'. The constraint is unnamed
--    in 001_init (Postgres auto-names it comment_type_check); drop + re-add.
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_type_check;
ALTER TABLE comment ADD CONSTRAINT comment_type_check
    CHECK (type IN ('comment', 'status_change', 'progress_update', 'system', 'wakeup'));

-- 2. Self-wakeup limits. NOT NULL with sane defaults so existing rows (and the
--    "no row -> default" path in the handler) behave identically to today.
ALTER TABLE cerebro_workspace_settings
    ADD COLUMN IF NOT EXISTS wakeup_max_self_per_issue INT NOT NULL DEFAULT 8,
    ADD COLUMN IF NOT EXISTS wakeup_min_interval_minutes INT NOT NULL DEFAULT 5;
