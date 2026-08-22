-- Add Prime Agent (`prime`) to the built-in runtime profile protocol whitelist.
-- Kept in lockstep with agent.SupportedTypes and agent.New().  NOT VALID
-- preserves the historical-row tolerance used by the prior family additions.
--
-- Rebased on migration 370 (add dim), so the rewritten CHECK keeps every family
-- that constraint already allowed instead of silently dropping one. This
-- statement replaces the whole constraint rather than amending it, so any
-- family missing from the list below is revoked -- `dim` (migration 370) and
-- `mcode` (migration 342) are therefore listed explicitly, not inherited.
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
        'dsh',
        'kiro',
        'antigravity',
        'qoder',
        'qoderclicn',
        'traecli',
        'deveco',
        'grok',
        'qwen',
        'qwenpaw',
        'mcode',
        'dim',
        'prime'
    )) NOT VALID;
