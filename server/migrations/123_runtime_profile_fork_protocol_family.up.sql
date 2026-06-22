-- Fork added four agent backends (wujieclaw, DeepSeek-TUI, qoderclicn, mmx)
-- to agent.SupportedTypes and agent.New(), but migration 120's
-- runtime_profile.protocol_family CHECK never listed them. The handler
-- (runtime_profile.go) validates protocol_family against agent.SupportedTypes
-- before INSERT, so a fork user creating a custom runtime profile based on
-- any of these backends passed the Go-side gate but hit a CHECK violation at
-- write time — the feature was unreachable for fork-only backends.
--
-- Keep this list in lockstep with agent.SupportedTypes and the test
-- TestSupportedTypesMatchesMigrationWhitelist.
ALTER TABLE runtime_profile
    DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check,
    ADD CONSTRAINT runtime_profile_protocol_family_check
        CHECK (protocol_family IN (
            'claude',
            'codebuddy',
            'codex',
            'copilot',
            'opencode',
            'openclaw',
            'wujieclaw',
            'hermes',
            'gemini',
            'pi',
            'cursor',
            'kimi',
            'kiro',
            'DeepSeek-TUI',
            'antigravity',
            'qoderclicn',
            'mmx'
        ));
