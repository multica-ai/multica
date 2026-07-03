-- Add 'deveco' (DevEco Code, Huawei's OpenCode fork) to the runtime_profile
-- protocol_family whitelist. Mirrors the drop/re-add NOT VALID pattern from
-- migration 126 so historical rows are not revalidated. Kept in lockstep with
-- agent.SupportedTypes and agent.New() (see server/pkg/agent/agent.go).
ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

ALTER TABLE runtime_profile ADD CONSTRAINT runtime_profile_protocol_family_check
    CHECK (protocol_family IN (
        'claude',
        'codebuddy',
        'codex',
        'copilot',
        'opencode',
        'deveco',
        'openclaw',
        'hermes',
        'pi',
        'cursor',
        'kimi',
        'kiro',
        'antigravity'
    )) NOT VALID;
