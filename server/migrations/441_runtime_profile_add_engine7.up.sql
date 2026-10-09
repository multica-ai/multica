-- Add Engine7 (栖) as a first-class runtime provider. Engine7 is the Twinsun
-- family of personal AI agents; its CLI is openclaw-protocol-compatible
-- (`engine7 agent --message … --json`), so it joins the whitelist as its own
-- family to allow independent detection and branding. NOT VALID preserves
-- historical-row tolerance while enforcing the expanded whitelist for new rows.
-- The whitelist includes zeroclaw (added by migration 403) and engine7.
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
        'engine7'
    )) NOT VALID;
