ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_transport_check;
ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

ALTER TABLE runtime_profile ADD CONSTRAINT runtime_profile_protocol_family_check
    CHECK (protocol_family IN (
        'claude', 'codebuddy', 'codex', 'copilot', 'opencode', 'openclaw',
        'hermes', 'pi', 'cursor', 'kimi', 'reasonix', 'dsh', 'kiro',
        'antigravity', 'qoder', 'qoderclicn', 'traecli', 'deveco', 'grok',
        'qwen', 'qwenpaw', 'mcode', 'dim', 'zeroclaw'
    )) NOT VALID;

ALTER TABLE runtime_profile
    DROP COLUMN IF EXISTS api_base_url,
    DROP COLUMN IF EXISTS credential_env,
    DROP COLUMN IF EXISTS default_model;
