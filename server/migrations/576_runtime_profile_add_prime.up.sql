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
-- 576 is above every version currently on main.  440, 441, 444, 446, 451,
-- 457, 468, 491, 500, 535, 545, 548 and 567 were each free when this migration
-- was renumbered onto them, and each was then taken: 440 by
-- 440_github_pr_head_sha_index (#7695), 441 by
-- 441_runtime_profile_add_codearts (#6985), 444 by
-- 444_comment_recovery_settled_at (#7820), which also added 445, 446 by
-- 446_issue_properties_bigm_index (#7878), which also added 447, 451 by
-- 451_agent_task_comment_thread (#8131), which also added 452, 457 by
-- 457_task_message_output_truncated (#8212), 468 by
-- 468_drop_reference_only_column (#8253), 491 by
-- 491_issue_status_category_backfill (#8466), 500 by
-- 500_task_message_call_id (#8567), 535 by
-- 535_github_pr_address_index (#8636), 545 by
-- 545_pr_auto_complete (#8758), which also added 546 and 547, 548 by
-- 548_task_supplement_comment_task_index (#8760), and 567 by
-- 567_channel_typing_reaction_retry_idx (#8656).  main (10a7e519d) now
-- reaches 572: 564 is 564_issue_property_list_types (#8559), and 565-572 are
-- the channel typing cleanup from #8656.  Open PR #8679 reserves 573-575,
-- so 576 is the next free prefix.  None of 442-575 is a family migration (501
-- adds the runtime_type column but leaves this constraint alone), so 441
-- remains this migration's predecessor in the chain and the family list
-- below is unchanged from the previous prefix.
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
