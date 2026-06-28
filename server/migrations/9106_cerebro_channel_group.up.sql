-- FIR-2159: add a third messaging kind, 'group', alongside 'channel' and 'dm'.
-- A group is a multi-party conversation with no fixed name (its title is derived
-- from its participants, like a DM) that sits with DMs in the inbox rather than
-- in the channel list, and can be converted to a named channel later. This is
-- the Slack model: dm (1:1) -> group (multi, unnamed) -> channel (named).
--
-- The kind CHECK constraint was created inline by the upstream migration
-- 061_issue_kind, so Postgres named it 'issue_kind_check'. We drop and re-add
-- it with 'group' included. The 9NNN namespace keeps this cerebro-only.

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_kind_check;

ALTER TABLE issue
    ADD CONSTRAINT issue_kind_check
    CHECK (kind IN ('issue', 'channel', 'dm', 'group'));
