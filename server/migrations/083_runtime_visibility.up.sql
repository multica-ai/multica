-- Upstream's 083 unconditionally ADDs the visibility column. Internal
-- installations already deployed this column under the now-reverted
-- 069_runtime_visibility migration with CHECK ('workspace','private')
-- and DEFAULT 'workspace'. Detect that state and converge to the
-- upstream shape (CHECK ('private','public'), DEFAULT 'private',
-- existing 'workspace' rows → 'public'). On a fresh install the column
-- does not exist and we fall through to upstream's vanilla ALTER.
DO $$
DECLARE
    old_check_name TEXT;
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'agent_runtime'
          AND column_name = 'visibility'
    ) THEN
        SELECT conname INTO old_check_name
        FROM pg_constraint
        WHERE conrelid = 'agent_runtime'::regclass
          AND contype = 'c'
          AND pg_get_constraintdef(oid) ILIKE '%visibility%';
        IF old_check_name IS NOT NULL THEN
            EXECUTE format('ALTER TABLE agent_runtime DROP CONSTRAINT %I', old_check_name);
        END IF;
        UPDATE agent_runtime SET visibility = 'public' WHERE visibility = 'workspace';
        ALTER TABLE agent_runtime
            ADD CONSTRAINT agent_runtime_visibility_check
            CHECK (visibility IN ('private', 'public'));
        ALTER TABLE agent_runtime ALTER COLUMN visibility SET DEFAULT 'private';
    ELSE
        ALTER TABLE agent_runtime
            ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private'
                CHECK (visibility IN ('private', 'public'));
    END IF;
END $$;
