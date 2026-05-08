-- Cerebro: scoped autopilots (workspace | personal | group).
-- See JEH-724. Group membership lookup lives in JEH-721's cerebro_group_member
-- table; this migration intentionally does NOT add a FK on group_id so it can
-- land independently. A follow-up migration adds the FK once cerebro_group is
-- in place.

ALTER TABLE autopilot
    ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'workspace'
        CHECK (scope IN ('workspace', 'personal', 'group'));

ALTER TABLE autopilot
    ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES "user"(id) ON DELETE SET NULL;

ALTER TABLE autopilot
    ADD COLUMN IF NOT EXISTS group_id UUID;

-- Scope-consistency: required identifier columns must be set for personal/group;
-- workspace scope must leave both NULL.
DO $$ BEGIN
    ALTER TABLE autopilot ADD CONSTRAINT autopilot_scope_consistency_check CHECK (
        (scope = 'workspace' AND owner_user_id IS NULL AND group_id IS NULL)
        OR (scope = 'personal' AND owner_user_id IS NOT NULL AND group_id IS NULL)
        OR (scope = 'group'    AND group_id      IS NOT NULL AND owner_user_id IS NULL)
    );
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS idx_autopilot_owner_user
    ON autopilot(workspace_id, owner_user_id) WHERE scope = 'personal';
CREATE INDEX IF NOT EXISTS idx_autopilot_group
    ON autopilot(workspace_id, group_id) WHERE scope = 'group';
