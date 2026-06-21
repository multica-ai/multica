-- FIR-1659 Fase 3: sharing for saved filters.
--
-- A saved filter (migration 9091) is owned by one member. Fase 3 lets the owner
-- widen who may USE it via a three-way visibility, mirroring the folder sharing
-- model the app already uses ("Only you / Selected colleagues / Whole team"):
--
--   private    — only the owner (the Fase 2 default; unchanged behaviour).
--   shared     — the owner plus the explicit member/group targets in
--                cerebro_saved_filter_shares.
--   workspace  — every member of the workspace.
--
-- Recipients get read/apply access only; just the owner can rename, re-share or
-- delete. The "who may create shared filters" group capability (Fase 4) reuses
-- the existing cerebro_group_capability table — no schema change is needed for
-- it, so it does not appear here.

ALTER TABLE cerebro_saved_filters
    ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'private'
    CHECK (visibility IN ('private', 'shared', 'workspace'));

CREATE TABLE IF NOT EXISTS cerebro_saved_filter_shares (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    saved_filter_id UUID NOT NULL REFERENCES cerebro_saved_filters(id) ON DELETE CASCADE,
    -- target_type: who the row grants access to. 'workspace' is expressed via
    -- the visibility column (no row), so shares only ever name a group or a
    -- single member. target_id is the group id or the user id accordingly.
    target_type     TEXT NOT NULL CHECK (target_type IN ('group', 'member')),
    target_id       UUID NOT NULL,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (saved_filter_id, target_type, target_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_saved_filter_shares_filter
    ON cerebro_saved_filter_shares (saved_filter_id);

-- Reverse lookup: "which filters are shared with this member/group" powers the
-- visible-to-user list query.
CREATE INDEX IF NOT EXISTS idx_cerebro_saved_filter_shares_target
    ON cerebro_saved_filter_shares (target_type, target_id);
