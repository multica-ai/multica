ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

-- Restore the pre-202 whitelist (migration 179 shape: with grok, without
-- mira). NOT VALID keeps the historical Gemini tolerance so the rollback
-- cannot fail on old rows.
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
        'antigravity',
        'qoder',
        'traecli',
        'deveco',
        'grok'
    )) NOT VALID;
