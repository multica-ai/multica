CREATE TABLE IF NOT EXISTS cerebro_group (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    created_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS cerebro_group_member (
    group_id UUID NOT NULL REFERENCES cerebro_group(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    added_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_group_member_group_user_unique UNIQUE(group_id, user_id)
);

ALTER TABLE cerebro_group
    ADD COLUMN IF NOT EXISTS description TEXT;

ALTER TABLE cerebro_group
    ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES "user"(id) ON DELETE SET NULL;

ALTER TABLE cerebro_group
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE cerebro_group
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

ALTER TABLE cerebro_group_member
    ADD COLUMN IF NOT EXISTS added_by UUID REFERENCES "user"(id) ON DELETE SET NULL;

ALTER TABLE cerebro_group_member
    ADD COLUMN IF NOT EXISTS added_at TIMESTAMPTZ NOT NULL DEFAULT now();

DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conrelid = 'cerebro_group_member'::regclass
          AND conname = 'cerebro_group_member_group_user_unique'
    ) THEN
        ALTER TABLE cerebro_group_member
            ADD CONSTRAINT cerebro_group_member_group_user_unique UNIQUE (group_id, user_id);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_cerebro_group_workspace
    ON cerebro_group (workspace_id);

CREATE INDEX IF NOT EXISTS idx_cerebro_group_member_user
    ON cerebro_group_member (user_id);
