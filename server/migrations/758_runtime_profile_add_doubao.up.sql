-- Expand the current whitelist (migration 656 shape) without scanning
-- historical rows. Replacing both parts in one ALTER keeps the constraint
-- present atomically.
--
-- Adds ByteDance's Doubao CLI (`doubao`), driven over ACP v1 via
-- `doubao acp serve`. NOT VALID mirrors 656 so a historical row outside the
-- whitelist cannot block the upgrade.
ALTER TABLE runtime_profile
    DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check,
    ADD CONSTRAINT runtime_profile_protocol_family_check
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
        'doubao',
        'qwen',
        'qwenpaw',
        'mcode',
        'muse',
        'dim',
        'zeroclaw',
        'devin',
        'zcode',
        'droid',
        'amp'
    )) NOT VALID;
