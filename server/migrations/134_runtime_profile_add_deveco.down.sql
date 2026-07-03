-- Reverse migration 134: remove 'deveco' from the runtime_profile
-- protocol_family whitelist, restoring the post-126 set.
ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

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
