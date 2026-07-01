-- Reverse 128_runtime_profile_protocol_family_omp.up.sql.
--
-- Restores the post-126 (drop_gemini) allow-list without 'omp', re-using
-- migration 126's NOT VALID semantics. A runtime_profile row already using
-- protocol_family = 'omp' is NOT validated by this rollback (NOT VALID skips
-- existing rows), but such rows would fail future writes — remove them before
-- downgrading.

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
