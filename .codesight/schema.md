# Schema

### user
- id: uuid (pk)
- name: text (required)
- email: text (unique)
- avatar_url: text

### workspace
- id: uuid (pk)
- name: text (required)
- slug: text (unique)
- description: text
- settings: jsonb (required)

### member
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- user_id: uuid (required, fk)
- role: text (required)

### agent
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- name: text (required)
- avatar_url: text
- runtime_mode: text (required)
- runtime_config: jsonb (required)
- visibility: text (required)
- status: text (required)
- max_concurrent_tasks: integer (required)
- owner_id: uuid (fk)

### issue
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- title: text (required)
- description: text
- status: text (required)
- priority: text (required)
- assignee_id: uuid (fk)
- creator_type: text (required)
- creator_id: uuid (required, fk)
- parent_issue_id: uuid (fk)
- acceptance_criteria: jsonb (required)
- context_refs: jsonb (required)
- position: float (required)
- due_date: timestamp(tz)

### issue_label
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- name: text (required)
- color: text (required)

### issue_to_label
- issue_id: uuid (required, fk)
- label_id: uuid (required, fk)

### issue_dependency
- id: uuid (pk)
- issue_id: uuid (required, fk)
- depends_on_issue_id: uuid (required, fk)
- type: text (required)

### comment
- id: uuid (pk)
- issue_id: uuid (required, fk)
- author_type: text (required)
- author_id: uuid (required, fk)
- content: text (required)
- type: text (required)

### inbox_item
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- recipient_type: text (required)
- recipient_id: uuid (required, fk)
- type: text (required)
- severity: text (required)
- issue_id: uuid (fk)
- title: text (required)
- body: text
- read: boolean (required)
- archived: boolean (required)

### agent_task_queue
- id: uuid (pk)
- agent_id: uuid (required, fk)
- issue_id: uuid (required, fk)
- status: text (required)
- priority: integer (required)
- dispatched_at: timestamp(tz)
- started_at: timestamp(tz)
- completed_at: timestamp(tz)
- result: jsonb
- error: text

### daemon_connection
- id: uuid (pk)
- agent_id: uuid (required, fk)
- daemon_id: text (required, fk)
- status: text (required)
- last_heartbeat_at: timestamp(tz)
- runtime_info: jsonb (required)

### activity_log
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- issue_id: uuid (fk)
- actor_id: uuid (fk)
- action: text (required)
- details: jsonb (required)

### agent_runtime
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- daemon_id: text (fk)
- name: text (required)
- runtime_mode: text (required)
- provider: text (required)
- status: text (required)
- device_info: text (required)
- metadata: jsonb (required)
- last_seen_at: timestamp(tz)

### daemon_pairing_session
- id: uuid (pk)
- token: text (required)
- daemon_id: text (required, fk)
- device_name: text (required)
- runtime_name: text (required)
- runtime_type: text (required)
- runtime_version: text (required)
- workspace_id: uuid (fk)
- approved_by: uuid (fk)
- status: text (required)
- approved_at: timestamp(tz)
- claimed_at: timestamp(tz)
- expires_at: timestamp(tz) (required)

### skill
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- name: text (required)
- description: text (required)
- content: text (required)
- config: jsonb (required)
- created_by: uuid (fk)

### skill_file
- id: uuid (pk)
- skill_id: uuid (required, fk)
- path: text (required)
- content: text (required)

### agent_skill
- agent_id: uuid (required, fk)
- skill_id: uuid (required, fk)

### verification_code
- id: uuid (pk)
- email: text (required)
- code: text (required)
- expires_at: timestamp(tz) (required)
- used: boolean (required)

### personal_access_token
- id: uuid (pk)
- user_id: uuid (required, fk)
- name: text (required)
- token_hash: text (required)
- token_prefix: text (required)
- expires_at: timestamp(tz)
- last_used_at: timestamp(tz)
- revoked: boolean (required)

### runtime_usage
- id: uuid (pk)
- runtime_id: uuid (required, fk)
- date: date (required)
- provider: text (required)
- model: text (required)
- input_tokens: bigint (required)
- output_tokens: bigint (required)
- cache_read_tokens: bigint (required)
- cache_write_tokens: bigint (required)

### issue_subscriber
- issue_id: uuid (required, fk)
- user_type: text (required)
- user_id: uuid (required, fk)
- reason: text (required)

### comment_reaction
- id: uuid (pk)
- comment_id: uuid (required, fk)
- workspace_id: uuid (required, fk)
- actor_type: text (required)
- actor_id: uuid (required, fk)
- emoji: text (required)

### task_message
- id: uuid (pk)
- task_id: uuid (required, fk)
- seq: integer (required)
- type: text (required)
- tool: text
- content: text
- input: jsonb
- output: text

### issue_reaction
- id: uuid (pk)
- issue_id: uuid (required, fk)
- workspace_id: uuid (required, fk)
- actor_type: text (required)
- actor_id: uuid (required, fk)
- emoji: text (required)

### attachment
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- issue_id: uuid (fk)
- comment_id: uuid (fk)
- uploader_type: text (required)
- uploader_id: uuid (required, fk)
- filename: text (required)
- url: text (required)
- content_type: text (required)
- size_bytes: bigint (required)

### daemon_token
- id: uuid (pk)
- token_hash: text (required)
- workspace_id: uuid (required, fk)
- daemon_id: text (required, fk)
- expires_at: timestamp(tz) (required)

### task_usage
- id: uuid (pk)
- task_id: uuid (required, fk)
- provider: text (required)
- model: text (required)
- input_tokens: bigint (required)
- output_tokens: bigint (required)
- cache_read_tokens: bigint (required)
- cache_write_tokens: bigint (required)

### chat_session
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- agent_id: uuid (required, fk)
- creator_id: uuid (required, fk)
- title: text (required)
- session_id: text (fk)
- work_dir: text
- status: text (required)

### chat_message
- id: uuid (pk)
- chat_session_id: uuid (required, fk)
- role: text (required)
- content: text (required)
- task_id: uuid (fk)

### project
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- title: text (required)
- description: text
- icon: text
- status: text (required)
- lead_id: uuid (fk)

### pinned_item
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- user_id: uuid (required, fk)
- item_type: text (required)
- item_id: uuid (required, fk)
- position: float (required)

### workspace_invitation
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- inviter_id: uuid (required, fk)
- invitee_email: text (required)
- invitee_user_id: uuid (fk)
- role: text (required)
- status: text (required)
- expires_at: timestamp(tz) (required)

### autopilot
- id: uuid (pk)
- workspace_id: uuid (required, fk)
- project_id: uuid (fk)
- title: text (required)
- description: text
- assignee_id: uuid (required, fk)
- priority: text (required)
- status: text (required)
- execution_mode: text (required)
- issue_title_template: text
- concurrency_policy: text (required)
- created_by_type: text (required)
- created_by_id: uuid (required, fk)
- last_run_at: timestamp(tz)

### autopilot_trigger
- id: uuid (pk)
- autopilot_id: uuid (required, fk)
- kind: text (required)
- enabled: boolean (required)
- cron_expression: text
- timezone: text (default)
- next_run_at: timestamp(tz)
- webhook_token: text
- label: text
- last_fired_at: timestamp(tz)

### autopilot_run
- id: uuid (pk)
- autopilot_id: uuid (required, fk)
- trigger_id: uuid (fk)
- source: text (required)
- status: text (required)
- issue_id: uuid (fk)
- task_id: uuid (fk)
- triggered_at: timestamp(tz) (required)
- completed_at: timestamp(tz)
- failure_reason: text
- trigger_payload: jsonb
- result: jsonb
