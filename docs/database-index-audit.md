# Database index audit

This report captures the MUL-6108 baseline generated from PostgreSQL 17.10 at
the branch base `254a02a79` after migrations 001–299. Re-run
`scripts/audit-redundant-indexes.sql` against a freshly migrated isolated
database before acting on it; prefix coverage is a candidate signal, not a drop
decision.

## Baseline

- Public-schema indexes: 346 before this change, 342 after migrations 300–303.
- Catalog candidates: 24 distinct indexes (32 candidate/covering pairs).
- Additional B-tree reverse-scan candidate: 1 (`idx_sys_cron_exec_job_plan`).
- First batch: remove 3 exact duplicates plus the verified reverse-scan
  candidate. Keep all 21 strict-prefix candidates for later production-plan and
  usage analysis.
- Keep `idx_task_usage_created_at_legacy`: it has a distinct
  `WHERE updated_at IS NULL` predicate and still supports the legacy branch of
  `rollup_task_usage_hourly_window`.

The catalog query requires matching access method, leading keys, opclasses,
collations, sort options, predicate and index expressions. `exact` rows match on
the full key; `prefix` rows match only on the candidate's complete left prefix.
All 24 catalog candidates below are B-tree indexes with no predicate or
expression; their candidate key columns use the same opclasses, collations and
sort options as the covering prefix. The `sys_cron` row is the sole ordering
exception and is documented separately below.

## Complete candidate list

| Table | Candidate | Relation | Covered by | Decision |
| --- | --- | --- | --- | --- |
| `agent` | `idx_agent_workspace` | prefix | `agent_workspace_name_unique`, `idx_agent_workspace_id_keyset` | keep pending evidence |
| `agent_invocation_target` | `agent_invocation_target_agent_id_idx` | prefix | `agent_invocation_target_agent_id_target_type_target_id_key` | keep pending evidence |
| `agent_runtime` | `idx_agent_runtime_workspace` | prefix | `idx_agent_runtime_status`, `idx_agent_runtime_workspace_id_keyset` | keep pending evidence |
| `agent_skill` | `idx_agent_skill_agent` | prefix | `agent_skill_pkey` | keep pending evidence |
| `agent_task_queue` | `idx_agent_task_queue_issue_id` | prefix | `idx_agent_task_queue_issue_id_keyset` | keep pending evidence |
| `channel_chat_session_binding` | `idx_channel_chat_session_binding_session` | exact | `channel_chat_session_binding_chat_session_id_key` | drop in 302 |
| `channel_installation` | `idx_channel_installation_workspace` | prefix | `channel_installation_workspace_id_agent_id_channel_type_key` | keep pending evidence |
| `comment_reaction` | `idx_comment_reaction_comment_id` | prefix | `comment_reaction_comment_id_actor_type_actor_id_emoji_key` | keep pending evidence |
| `github_installation` | `idx_github_installation_workspace` | prefix | `github_installation_workspace_id_installation_id_key` | keep pending evidence |
| `github_pull_request` | `idx_github_pull_request_workspace` | prefix | `github_pull_request_workspace_id_repo_owner_repo_name_pr_nu_key` | keep pending evidence |
| `issue` | `idx_issue_workspace` | prefix | `idx_issue_status`, `idx_issue_workspace_assignee`, `idx_issue_workspace_id_keyset`, `idx_issue_workspace_number`, `idx_issue_workspace_parent`, `idx_issue_workspace_position`, `uq_issue_workspace_number` | keep pending evidence |
| `issue` | `idx_issue_workspace_number` | exact | `uq_issue_workspace_number` | drop in 300 |
| `issue_reaction` | `idx_issue_reaction_issue_id` | prefix | `issue_reaction_issue_id_actor_type_actor_id_emoji_key` | keep pending evidence |
| `lark_chat_session_binding` | `idx_lark_chat_session_binding_session` | exact | `lark_chat_session_binding_chat_session_id_key` | drop in 303 |
| `lark_installation` | `idx_lark_installation_workspace` | prefix | `lark_installation_workspace_id_agent_id_key` | keep pending evidence |
| `member` | `idx_member_workspace` | prefix | `member_workspace_id_user_id_key` | keep pending evidence |
| `runtime_profile` | `idx_runtime_profile_workspace` | prefix | `runtime_profile_workspace_id_display_name_key` | keep pending evidence |
| `skill` | `idx_skill_workspace` | prefix | `skill_workspace_id_name_key` | keep pending evidence |
| `skill_file` | `idx_skill_file_skill` | prefix | `skill_file_skill_id_path_key` | keep pending evidence |
| `squad_member` | `idx_squad_member_squad` | prefix | `squad_member_squad_id_member_type_member_id_key` | keep pending evidence |
| `task_usage` | `idx_task_usage_task_id` | prefix | `task_usage_task_id_provider_model_key` | keep pending evidence |
| `vcs_commit_status` | `idx_vcs_commit_status_lookup` | prefix | `vcs_commit_status_pkey` | keep pending evidence |
| `vcs_connection` | `idx_vcs_connection_workspace` | prefix | `vcs_connection_workspace_id_instance_url_key` | keep pending evidence |
| `vcs_pull_request` | `idx_vcs_pull_request_connection` | prefix | `vcs_pull_request_connection_id_repo_owner_repo_name_pr_numb_key` | keep pending evidence |
| `sys_cron_executions` | `idx_sys_cron_exec_job_plan` | reverse scan | `uq_sys_cron_execution` | drop in 301 |

`idx_sys_cron_exec_job_plan` is intentionally outside the strict catalog
matches: its final key is `plan_time DESC`, while `uq_sys_cron_execution` uses
the default ascending order. The scheduler constrains the preceding three keys
by equality, so PostgreSQL can satisfy `ORDER BY plan_time DESC LIMIT 1` with an
`Index Scan Backward` over the unique index. The migration regression test
asserts that plan explicitly.

## Rollout and rollback evidence

Each drop is a standalone `DROP INDEX CONCURRENTLY` migration. Its down file is
a standalone `CREATE INDEX CONCURRENTLY` with the original definition. The
migration runner now executes a down-direction INVALID-index cleanup hook before
retrying those creates, so an interrupted rollback cannot be recorded while
leaving an unusable index.

The isolated regression fixture records the structural before/after comparison:

| Measure | Before | After |
| --- | ---: | ---: |
| Public-schema index count | 346 | 342 |
| Redundant B-tree maintenance per affected insert/indexed-key update | 1 | 0 |
| Covering/constraint indexes retained | 4 | 4 |

Disk reclaimed in an environment is the sum of `pg_relation_size` for the four
dropped indexes; it depends on row count and value distribution. The migration
test measures this sum on its seeded fixture before the drops and verifies that
all four relations disappear afterward. Production should record those sizes
before rollout rather than extrapolating a synthetic byte count; the reusable
audit script intentionally does not hardcode a batch that disappears after the
cleanup lands.
