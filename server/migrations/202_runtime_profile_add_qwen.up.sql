ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

-- Add Qwen Code (`qwen`), driven by `qwen -p <prompt> --output-format
-- stream-json`. NOT VALID preserves the historical-row upgrade behaviour of
-- the earlier protocol-family widening migrations.
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
        'qwen'
    )) NOT VALID;
