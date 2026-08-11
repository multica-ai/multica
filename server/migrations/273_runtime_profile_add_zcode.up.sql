-- Add ZCode (`zcode`) to the built-in runtime profile protocol whitelist.
-- zcode (zcode-cli, https://github.com/kingsword09/zcode-cli) is a terminal
-- client for the ZCode Desktop agent runtime; multica drives it headlessly via
-- `zcode --prompt <text> --json`. Kept in lockstep with agent.SupportedTypes
-- and agent.New(). NOT VALID preserves the historical-row tolerance used by
-- the prior family additions.
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
        'reasonix',
        'kiro',
        'antigravity',
        'qoder',
        'qoderclicn',
        'traecli',
        'deveco',
        'grok',
        'qwen',
        'qwenpaw',
        'zcode'
    )) NOT VALID;
