-- Reverse all multica_ prefix renames

-- Core identity & access
ALTER TABLE multica_user RENAME TO "user";
ALTER TABLE multica_member RENAME TO member;
ALTER TABLE multica_workspace RENAME TO workspace;
ALTER TABLE multica_workspace_invitation RENAME TO workspace_invitation;
ALTER TABLE multica_personal_access_token RENAME TO personal_access_token;
ALTER TABLE multica_verification_code RENAME TO verification_code;

-- Agents & runtimes
ALTER TABLE multica_agent RENAME TO agent;
ALTER TABLE multica_agent_runtime RENAME TO agent_runtime;
ALTER TABLE multica_daemon_connection RENAME TO daemon_connection;
ALTER TABLE multica_daemon_token RENAME TO daemon_token;
ALTER TABLE multica_squad RENAME TO squad;
ALTER TABLE multica_squad_member RENAME TO squad_member;

-- Issues & projects
ALTER TABLE multica_issue RENAME TO issue;
ALTER TABLE multica_issue_label RENAME TO issue_label;
ALTER TABLE multica_issue_to_label RENAME TO issue_to_label;
ALTER TABLE multica_issue_dependency RENAME TO issue_dependency;
ALTER TABLE multica_issue_reaction RENAME TO issue_reaction;
ALTER TABLE multica_issue_subscriber RENAME TO issue_subscriber;
ALTER TABLE multica_issue_pull_request RENAME TO issue_pull_request;
ALTER TABLE multica_project RENAME TO project;
ALTER TABLE multica_project_resource RENAME TO project_resource;
ALTER TABLE multica_comment RENAME TO comment;
ALTER TABLE multica_comment_reaction RENAME TO comment_reaction;
ALTER TABLE multica_attachment RENAME TO attachment;

-- Task queue & execution
ALTER TABLE multica_agent_task_queue RENAME TO agent_task_queue;
ALTER TABLE multica_task_usage RENAME TO task_usage;
ALTER TABLE multica_task_usage_hourly RENAME TO task_usage_hourly;
ALTER TABLE multica_task_usage_hourly_dirty RENAME TO task_usage_hourly_dirty;
ALTER TABLE multica_task_usage_hourly_rollup_state RENAME TO task_usage_hourly_rollup_state;
ALTER TABLE multica_task_message RENAME TO task_message;

-- Chat
ALTER TABLE multica_chat_session RENAME TO chat_session;
ALTER TABLE multica_chat_message RENAME TO chat_message;

-- Workflows
ALTER TABLE multica_workflow RENAME TO workflow;
ALTER TABLE multica_workflow_node RENAME TO workflow_node;
ALTER TABLE multica_workflow_edge RENAME TO workflow_edge;
ALTER TABLE multica_workflow_run RENAME TO workflow_run;
ALTER TABLE multica_workflow_node_run RENAME TO workflow_node_run;

-- Autopilot
ALTER TABLE multica_autopilot RENAME TO autopilot;
ALTER TABLE multica_autopilot_run RENAME TO autopilot_run;
ALTER TABLE multica_autopilot_trigger RENAME TO autopilot_trigger;
ALTER TABLE multica_webhook_delivery RENAME TO webhook_delivery;

-- GitHub integration
ALTER TABLE multica_github_installation RENAME TO github_installation;
ALTER TABLE multica_github_pull_request RENAME TO github_pull_request;
ALTER TABLE multica_github_pull_request_check_suite RENAME TO github_pull_request_check_suite;

-- Inbox & activity
ALTER TABLE multica_inbox_item RENAME TO inbox_item;
ALTER TABLE multica_activity_log RENAME TO activity_log;

-- Skills
ALTER TABLE multica_skill RENAME TO skill;
ALTER TABLE multica_skill_file RENAME TO skill_file;
ALTER TABLE multica_agent_skill RENAME TO agent_skill;

-- Other
ALTER TABLE multica_pinned_item RENAME TO pinned_item;
ALTER TABLE multica_feedback RENAME TO feedback;
ALTER TABLE multica_contact_sales_inquiry RENAME TO contact_sales_inquiry;
ALTER TABLE multica_notification_preference RENAME TO notification_preference;
