ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

-- Restore the post-136 whitelist (qoder + traecli included, droid excluded).
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
        'traecli'
    )) NOT VALID;