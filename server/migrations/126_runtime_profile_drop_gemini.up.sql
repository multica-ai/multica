ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

-- Enforce the new whitelist for future writes without blocking upgrades for
-- workspaces that already have historical Gemini custom runtime profiles.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'runtime_profile_protocol_family_check'
          AND conrelid = 'runtime_profile'::regclass
    ) THEN
        ALTER TABLE runtime_profile ADD CONSTRAINT runtime_profile_protocol_family_check
            CHECK (protocol_family IN (
                'claude',
                'codebuddy',
                'codex',
                'copilot',
                'opencode',
                'openclaw',
                'hermes',
                'pi',
                'cursor',
                'kimi',
                'kiro',
                'antigravity'
            )) NOT VALID;
    END IF;
END
$$;
