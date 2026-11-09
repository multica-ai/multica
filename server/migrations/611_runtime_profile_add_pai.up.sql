-- Add P-ai (https://github.com/kawayiYokami/P-ai) as a first-party ACP
-- protocol family. NOT VALID preserves historical-row tolerance while
-- enforcing the expanded whitelist for new rows. The whitelist below matches
-- the state left by migration 610 (which added zcode) plus pai.
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
        'qwen',
        'qwenpaw',
        'mcode',
        'muse',
        'dim',
        'zeroclaw',
        'devin',
        'zcode',
        'pai'
    )) NOT VALID;
