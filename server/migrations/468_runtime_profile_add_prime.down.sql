-- Revert Prime Agent (`prime`) from the built-in runtime profile protocol
-- whitelist, restoring migration 441's family set exactly -- the state this
-- migration was applied on top of, which already includes `codearts`
-- (migration 441), `zeroclaw` (migration 403), `dim` (migration 370) and
-- `mcode` (migration 342).
-- Only `prime` is removed; no earlier family is revoked.
ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

ALTER TABLE runtime_profile ADD CONSTRAINT runtime_profile_protocol_family_check
    CHECK (protocol_family IN (
        'claude',
        'codebuddy',
        'codex',
        'copilot',
        'opencode',
        'codearts',
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
        'zeroclaw'
    )) NOT VALID;
