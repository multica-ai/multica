-- Add Prime Agent (`prime`) to the built-in runtime profile protocol whitelist.
-- Kept in lockstep with agent.SupportedTypes and agent.New().  NOT VALID
-- preserves the historical-row tolerance used by the prior family additions.
--
-- Rebased onto migration 441 (add codearts), the newest family migration on
-- main.  This statement replaces the whole constraint rather than amending it,
-- so any family missing from the list below is revoked: `codearts`
-- (migration 441), `zeroclaw` (migration 403), `dim` (migration 370) and
-- `mcode` (migration 342) are therefore listed explicitly, not inherited.
--
-- The number matters as much as the list.  The migration runner applies
-- versions out of order (see internal/migrations.AllVersions), so a prefix
-- below 441 would run *after* codearts on any database that already applied
-- it, and this rewritten CHECK would silently revoke the codearts family.
-- 451 is above every version currently on main.  440, 441, 444 and 446 were
-- each free when this migration was renumbered onto them, and each was then
-- taken: 440 by 440_github_pr_head_sha_index (#7695), 441 by
-- 441_runtime_profile_add_codearts (#6985), 444 by
-- 444_comment_recovery_settled_at (#7820), which also added 445, and 446 by
-- 446_issue_properties_bigm_index (#7878), which also added 447.  main now
-- reaches 450, so 451 is the next free prefix.  Only 441 is a family
-- migration, so it remains this migration's predecessor in the chain.
ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

ALTER TABLE runtime_profile ADD CONSTRAINT runtime_profile_protocol_family_check
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
        'dim',
        'zeroclaw',
        'prime'
    )) NOT VALID;
