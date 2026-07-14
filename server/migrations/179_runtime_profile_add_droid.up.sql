-- Widen the protocol_family whitelist to include Factory Droid so users can
-- base a custom runtime profile on the Droid backend (factory.ai runtime).
-- Builds on migration 136 (traecli) and migration 175 (deveco); includes
-- deveco so this single CHECK stays the canonical whitelist. Mirrors the NOT
-- VALID form so a historical row not yet satisfying the wider constraint does
-- not block the upgrade.
ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

ALTER TABLE runtime_profile ADD CONSTRAINT runtime_profile_protocol_family_check
    CHECK (protocol_family IN (
        'claude',
        'codebuddy',
        'codex',
        'copilot',
        'droid',
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
        'deveco'
    )) NOT VALID;