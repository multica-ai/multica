-- API-backed runtime profiles. The credential is never stored here; only the
-- daemon environment variable name that owns it is persisted.
ALTER TABLE runtime_profile
    ADD COLUMN api_base_url TEXT,
    ADD COLUMN credential_env TEXT,
    ADD COLUMN default_model TEXT;

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
        'zeroclaw',
        'opencode-api',
        'opencode-zen',
        'opencode-go',
        'openrouter',
        'vercel-ai-gateway',
        'ollama',
        'lmstudio',
        'nvidia-nim'
    )) NOT VALID;

ALTER TABLE runtime_profile ADD CONSTRAINT runtime_profile_transport_check
    CHECK (
        (
            protocol_family IN (
                'claude', 'codebuddy', 'codex', 'copilot', 'opencode',
                'openclaw', 'hermes', 'pi', 'cursor', 'kimi', 'reasonix',
                'dsh', 'kiro', 'antigravity', 'qoder', 'qoderclicn',
                'traecli', 'deveco', 'grok', 'qwen', 'qwenpaw', 'mcode',
                'dim', 'zeroclaw'
            )
            AND command_name <> ''
            AND api_base_url IS NULL
            AND credential_env IS NULL
            AND default_model IS NULL
        )
        OR (
            protocol_family IN (
                'opencode-api', 'opencode-zen', 'opencode-go', 'openrouter',
                'vercel-ai-gateway', 'ollama', 'lmstudio', 'nvidia-nim'
            )
            AND command_name = ''
            AND fixed_args = '[]'::jsonb
        )
    ) NOT VALID;
