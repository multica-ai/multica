-- Add Qoder CN CLI (`qoderclicn`) as a protocol family. It reuses the Qoder
-- ACP backend while retaining a distinct command, account, and config root.
-- NOT VALID preserves the historical-row tolerance used by prior family
-- additions.
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
        'qoderclicn',
        'traecli',
        'deveco',
        'grok',
        'qwen'
    )) NOT VALID;
