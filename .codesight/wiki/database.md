# Database

> **Navigation aid.** Schema shapes and field types extracted via AST. Read the actual schema source files before writing migrations or query logic.

**unknown** — 36 models

### user

pk: `id` (uuid)

- `id`: uuid _(pk)_
- `name`: text _(required)_
- `email`: text _(unique)_
- `avatar_url`: text

### workspace

pk: `id` (uuid)

- `id`: uuid _(pk)_
- `name`: text _(required)_
- `slug`: text _(unique)_
- `description`: text
- `settings`: jsonb _(required)_

### member

pk: `id` (uuid) · fk: workspace_id, user_id

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `user_id`: uuid _(required, fk)_
- `role`: text _(required)_

### agent

pk: `id` (uuid) · fk: workspace_id, owner_id

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `name`: text _(required)_
- `avatar_url`: text
- `runtime_mode`: text _(required)_
- `runtime_config`: jsonb _(required)_
- `visibility`: text _(required)_
- `status`: text _(required)_
- `max_concurrent_tasks`: integer _(required)_
- `owner_id`: uuid _(fk)_

### issue

pk: `id` (uuid) · fk: workspace_id, assignee_id, creator_id, parent_issue_id

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `title`: text _(required)_
- `description`: text
- `status`: text _(required)_
- `priority`: text _(required)_
- `assignee_id`: uuid _(fk)_
- `creator_type`: text _(required)_
- `creator_id`: uuid _(required, fk)_
- `parent_issue_id`: uuid _(fk)_
- `acceptance_criteria`: jsonb _(required)_
- `context_refs`: jsonb _(required)_
- `position`: float _(required)_
- `due_date`: timestamp(tz)

### issue_label

pk: `id` (uuid) · fk: workspace_id

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `name`: text _(required)_
- `color`: text _(required)_

### issue_to_label

fk: issue_id, label_id

- `issue_id`: uuid _(required, fk)_
- `label_id`: uuid _(required, fk)_

### issue_dependency

pk: `id` (uuid) · fk: issue_id, depends_on_issue_id

- `id`: uuid _(pk)_
- `issue_id`: uuid _(required, fk)_
- `depends_on_issue_id`: uuid _(required, fk)_
- `type`: text _(required)_

### comment

pk: `id` (uuid) · fk: issue_id, author_id

- `id`: uuid _(pk)_
- `issue_id`: uuid _(required, fk)_
- `author_type`: text _(required)_
- `author_id`: uuid _(required, fk)_
- `content`: text _(required)_
- `type`: text _(required)_

### inbox_item

pk: `id` (uuid) · fk: workspace_id, recipient_id, issue_id

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `recipient_type`: text _(required)_
- `recipient_id`: uuid _(required, fk)_
- `type`: text _(required)_
- `severity`: text _(required)_
- `issue_id`: uuid _(fk)_
- `title`: text _(required)_
- `body`: text
- `read`: boolean _(required)_
- `archived`: boolean _(required)_

### agent_task_queue

pk: `id` (uuid) · fk: agent_id, issue_id

- `id`: uuid _(pk)_
- `agent_id`: uuid _(required, fk)_
- `issue_id`: uuid _(required, fk)_
- `status`: text _(required)_
- `priority`: integer _(required)_
- `dispatched_at`: timestamp(tz)
- `started_at`: timestamp(tz)
- `completed_at`: timestamp(tz)
- `result`: jsonb
- `error`: text

### daemon_connection

pk: `id` (uuid) · fk: agent_id, daemon_id

- `id`: uuid _(pk)_
- `agent_id`: uuid _(required, fk)_
- `daemon_id`: text _(required, fk)_
- `status`: text _(required)_
- `last_heartbeat_at`: timestamp(tz)
- `runtime_info`: jsonb _(required)_

### activity_log

pk: `id` (uuid) · fk: workspace_id, issue_id, actor_id

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `issue_id`: uuid _(fk)_
- `actor_id`: uuid _(fk)_
- `action`: text _(required)_
- `details`: jsonb _(required)_

### agent_runtime

pk: `id` (uuid) · fk: workspace_id, daemon_id

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `daemon_id`: text _(fk)_
- `name`: text _(required)_
- `runtime_mode`: text _(required)_
- `provider`: text _(required)_
- `status`: text _(required)_
- `device_info`: text _(required)_
- `metadata`: jsonb _(required)_
- `last_seen_at`: timestamp(tz)

### daemon_pairing_session

pk: `id` (uuid) · fk: daemon_id, workspace_id, approved_by

- `id`: uuid _(pk)_
- `token`: text _(required)_
- `daemon_id`: text _(required, fk)_
- `device_name`: text _(required)_
- `runtime_name`: text _(required)_
- `runtime_type`: text _(required)_
- `runtime_version`: text _(required)_
- `workspace_id`: uuid _(fk)_
- `approved_by`: uuid _(fk)_
- `status`: text _(required)_
- `approved_at`: timestamp(tz)
- `claimed_at`: timestamp(tz)
- `expires_at`: timestamp(tz) _(required)_

### skill

pk: `id` (uuid) · fk: workspace_id, created_by

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `name`: text _(required)_
- `description`: text _(required)_
- `content`: text _(required)_
- `config`: jsonb _(required)_
- `created_by`: uuid _(fk)_

### skill_file

pk: `id` (uuid) · fk: skill_id

- `id`: uuid _(pk)_
- `skill_id`: uuid _(required, fk)_
- `path`: text _(required)_
- `content`: text _(required)_

### agent_skill

fk: agent_id, skill_id

- `agent_id`: uuid _(required, fk)_
- `skill_id`: uuid _(required, fk)_

### verification_code

pk: `id` (uuid)

- `id`: uuid _(pk)_
- `email`: text _(required)_
- `code`: text _(required)_
- `expires_at`: timestamp(tz) _(required)_
- `used`: boolean _(required)_

### personal_access_token

pk: `id` (uuid) · fk: user_id

- `id`: uuid _(pk)_
- `user_id`: uuid _(required, fk)_
- `name`: text _(required)_
- `token_hash`: text _(required)_
- `token_prefix`: text _(required)_
- `expires_at`: timestamp(tz)
- `last_used_at`: timestamp(tz)
- `revoked`: boolean _(required)_

### runtime_usage

pk: `id` (uuid) · fk: runtime_id

- `id`: uuid _(pk)_
- `runtime_id`: uuid _(required, fk)_
- `date`: date _(required)_
- `provider`: text _(required)_
- `model`: text _(required)_
- `input_tokens`: bigint _(required)_
- `output_tokens`: bigint _(required)_
- `cache_read_tokens`: bigint _(required)_
- `cache_write_tokens`: bigint _(required)_

### issue_subscriber

fk: issue_id, user_id

- `issue_id`: uuid _(required, fk)_
- `user_type`: text _(required)_
- `user_id`: uuid _(required, fk)_
- `reason`: text _(required)_

### comment_reaction

pk: `id` (uuid) · fk: comment_id, workspace_id, actor_id

- `id`: uuid _(pk)_
- `comment_id`: uuid _(required, fk)_
- `workspace_id`: uuid _(required, fk)_
- `actor_type`: text _(required)_
- `actor_id`: uuid _(required, fk)_
- `emoji`: text _(required)_

### task_message

pk: `id` (uuid) · fk: task_id

- `id`: uuid _(pk)_
- `task_id`: uuid _(required, fk)_
- `seq`: integer _(required)_
- `type`: text _(required)_
- `tool`: text
- `content`: text
- `input`: jsonb
- `output`: text

### issue_reaction

pk: `id` (uuid) · fk: issue_id, workspace_id, actor_id

- `id`: uuid _(pk)_
- `issue_id`: uuid _(required, fk)_
- `workspace_id`: uuid _(required, fk)_
- `actor_type`: text _(required)_
- `actor_id`: uuid _(required, fk)_
- `emoji`: text _(required)_

### attachment

pk: `id` (uuid) · fk: workspace_id, issue_id, comment_id, uploader_id

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `issue_id`: uuid _(fk)_
- `comment_id`: uuid _(fk)_
- `uploader_type`: text _(required)_
- `uploader_id`: uuid _(required, fk)_
- `filename`: text _(required)_
- `url`: text _(required)_
- `content_type`: text _(required)_
- `size_bytes`: bigint _(required)_

### daemon_token

pk: `id` (uuid) · fk: workspace_id, daemon_id

- `id`: uuid _(pk)_
- `token_hash`: text _(required)_
- `workspace_id`: uuid _(required, fk)_
- `daemon_id`: text _(required, fk)_
- `expires_at`: timestamp(tz) _(required)_

### task_usage

pk: `id` (uuid) · fk: task_id

- `id`: uuid _(pk)_
- `task_id`: uuid _(required, fk)_
- `provider`: text _(required)_
- `model`: text _(required)_
- `input_tokens`: bigint _(required)_
- `output_tokens`: bigint _(required)_
- `cache_read_tokens`: bigint _(required)_
- `cache_write_tokens`: bigint _(required)_

### chat_session

pk: `id` (uuid) · fk: workspace_id, agent_id, creator_id, session_id

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `agent_id`: uuid _(required, fk)_
- `creator_id`: uuid _(required, fk)_
- `title`: text _(required)_
- `session_id`: text _(fk)_
- `work_dir`: text
- `status`: text _(required)_

### chat_message

pk: `id` (uuid) · fk: chat_session_id, task_id

- `id`: uuid _(pk)_
- `chat_session_id`: uuid _(required, fk)_
- `role`: text _(required)_
- `content`: text _(required)_
- `task_id`: uuid _(fk)_

### project

pk: `id` (uuid) · fk: workspace_id, lead_id

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `title`: text _(required)_
- `description`: text
- `icon`: text
- `status`: text _(required)_
- `lead_id`: uuid _(fk)_

### pinned_item

pk: `id` (uuid) · fk: workspace_id, user_id, item_id

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `user_id`: uuid _(required, fk)_
- `item_type`: text _(required)_
- `item_id`: uuid _(required, fk)_
- `position`: float _(required)_

### workspace_invitation

pk: `id` (uuid) · fk: workspace_id, inviter_id, invitee_user_id

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `inviter_id`: uuid _(required, fk)_
- `invitee_email`: text _(required)_
- `invitee_user_id`: uuid _(fk)_
- `role`: text _(required)_
- `status`: text _(required)_
- `expires_at`: timestamp(tz) _(required)_

### autopilot

pk: `id` (uuid) · fk: workspace_id, project_id, assignee_id, created_by_id

- `id`: uuid _(pk)_
- `workspace_id`: uuid _(required, fk)_
- `project_id`: uuid _(fk)_
- `title`: text _(required)_
- `description`: text
- `assignee_id`: uuid _(required, fk)_
- `priority`: text _(required)_
- `status`: text _(required)_
- `execution_mode`: text _(required)_
- `issue_title_template`: text
- `concurrency_policy`: text _(required)_
- `created_by_type`: text _(required)_
- `created_by_id`: uuid _(required, fk)_
- `last_run_at`: timestamp(tz)

### autopilot_trigger

pk: `id` (uuid) · fk: autopilot_id

- `id`: uuid _(pk)_
- `autopilot_id`: uuid _(required, fk)_
- `kind`: text _(required)_
- `enabled`: boolean _(required)_
- `cron_expression`: text
- `timezone`: text _(default)_
- `next_run_at`: timestamp(tz)
- `webhook_token`: text
- `label`: text
- `last_fired_at`: timestamp(tz)

### autopilot_run

pk: `id` (uuid) · fk: autopilot_id, trigger_id, issue_id, task_id

- `id`: uuid _(pk)_
- `autopilot_id`: uuid _(required, fk)_
- `trigger_id`: uuid _(fk)_
- `source`: text _(required)_
- `status`: text _(required)_
- `issue_id`: uuid _(fk)_
- `task_id`: uuid _(fk)_
- `triggered_at`: timestamp(tz) _(required)_
- `completed_at`: timestamp(tz)
- `failure_reason`: text
- `trigger_payload`: jsonb
- `result`: jsonb

## Schema Source Files

Search for ORM schema declarations:
- Drizzle: `pgTable` / `mysqlTable` / `sqliteTable`
- Prisma: `prisma/schema.prisma`
- TypeORM: `@Entity()` decorator
- SQLAlchemy: class inheriting `Base`

---
_Back to [overview.md](./overview.md)_