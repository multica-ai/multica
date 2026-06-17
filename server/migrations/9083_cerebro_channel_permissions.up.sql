-- Per-channel permission settings (TECH-3698). Channels are issues with
-- kind='channel'; the absence of a row means "defaults": only admins/owners
-- may rename, anyone may add other participants, and members may leave on
-- their own. A row exists only once the creator (or an admin later) deviates
-- from those defaults — but we also write one on channel creation so the
-- creator's explicit choices are preserved. DMs are never gated.
CREATE TABLE IF NOT EXISTS cerebro_channel_permissions (
    channel_id          uuid PRIMARY KEY REFERENCES issue(id) ON DELETE CASCADE,
    rename_policy       text NOT NULL DEFAULT 'admins'   CHECK (rename_policy IN ('admins', 'everyone')),
    add_members_policy  text NOT NULL DEFAULT 'everyone' CHECK (add_members_policy IN ('admins', 'everyone')),
    allow_self_leave    boolean NOT NULL DEFAULT true,
    updated_at          timestamptz NOT NULL DEFAULT NOW()
);
