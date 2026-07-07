ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

-- Widen the whitelist to include OMP (oh-my-pi) so users can base a custom
-- runtime profile on the existing OMP backend (launches `<command> acp --yolo`
-- over the standard ACP transport) instead of misrouting through another ACP
-- family with incompatible arguments. NOT VALID mirrors migration 126/134/136
-- so a historical Gemini row they intentionally tolerated does not block the
-- upgrade.
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
        'omp'
    )) NOT VALID;
