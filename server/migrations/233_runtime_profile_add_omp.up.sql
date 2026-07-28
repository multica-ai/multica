-- Add OMP (oh-my-pi) to the built-in runtime profile protocol whitelist so
-- users can base a custom runtime profile on the existing OMP backend
-- (launches `omp acp --yolo` over the standard ACP transport) instead of
-- misrouting through another ACP family with incompatible arguments. Builds on
-- migration 202's shape (claude..qwen), so deveco/grok/qwen stay listed too.
-- Kept in lockstep with agent.SupportedTypes and agent.New(). NOT VALID
-- preserves the historical-row tolerance used by the prior family additions.
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
        'antigravity',
        'qoder',
        'traecli',
        'deveco',
        'grok',
        'qwen',
        'omp'
    )) NOT VALID;
