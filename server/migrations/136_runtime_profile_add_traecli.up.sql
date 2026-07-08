ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

-- Widen the whitelist to include Trae (`traecli`). Trae already has a New()
-- backend, launch header (`traecli acp serve`) and provider branding, but was
-- missing from the protocol_family whitelist, so custom runtime profiles based
-- on Trae were rejected and it never appeared in the family picker (#4945).
-- NOT VALID mirrors migrations 126/134 so a historical Gemini row they
-- intentionally tolerated does not block the upgrade.
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
        'traecli'
    )) NOT VALID;
