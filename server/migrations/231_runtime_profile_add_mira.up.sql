ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

-- Widen the whitelist to include Mira (`mira`), driven by the `mircli`
-- Python CLI in single-shot `-p --output-format stream-json -y` mode.
-- Mira is positioned as a memory-oriented conversational agent in the
-- Multica daemon (project Q&A, PRD drafting, long-term memory, decision
-- rubber-ducking); it is NOT a code-mutation runtime. Builds on
-- migration 179's shape (which added `grok`). NOT VALID mirrors
-- migrations 126/134/136/175/179 so a historical Gemini row they
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
        'traecli',
        'deveco',
        'grok',
        'qwen',
        'mira'
    )) NOT VALID;
