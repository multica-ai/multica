-- Revert the fork-only protocol_family additions. Restore migration 120's
-- original narrower whitelist.
ALTER TABLE runtime_profile
    DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check,
    ADD CONSTRAINT runtime_profile_protocol_family_check
        CHECK (protocol_family IN (
            'claude',
            'codebuddy',
            'codex',
            'copilot',
            'opencode',
            'openclaw',
            'hermes',
            'gemini',
            'pi',
            'cursor',
            'kimi',
            'kiro',
            'antigravity'
        ));
