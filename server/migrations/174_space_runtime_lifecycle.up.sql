-- Track only lifecycle changes caused by archiving a Space. This lets restore
-- preserve resources that a user had already archived/paused independently.
ALTER TABLE squad
    ADD COLUMN archived_by_space_at TIMESTAMPTZ;

ALTER TABLE autopilot
    ADD COLUMN paused_by_space_at TIMESTAMPTZ,
    ADD COLUMN status_before_space_archive TEXT
        CHECK (status_before_space_archive IN ('active', 'paused'));

CREATE INDEX idx_squad_archived_by_space
    ON squad(space_id)
    WHERE archived_by_space_at IS NOT NULL;

CREATE INDEX idx_autopilot_paused_by_space
    ON autopilot(space_id)
    WHERE paused_by_space_at IS NOT NULL;

COMMENT ON COLUMN squad.archived_by_space_at IS
    'Set only when the owning Space archives the Squad; used for non-destructive Space restore.';
COMMENT ON COLUMN autopilot.paused_by_space_at IS
    'Set only when the owning Space auto-pauses an active Autopilot; requires explicit resume after Space restore.';
