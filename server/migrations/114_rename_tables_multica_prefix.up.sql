-- Rename all Multica tables with multica_ prefix so they can coexist
-- with costrict-web tables in a shared PostgreSQL instance.
-- PostgreSQL automatically renames FK constraints, indexes, and sequences.

-- Core identity & access
ALTER TABLE "user" RENAME TO multica_user;
ALTER TABLE member RENAME TO multica_member;
ALTER TABLE workspace RENAME TO multica_workspace;
ALTER TABLE workspace_invitation RENAME TO multica_workspace_invitation;
ALTER TABLE personal_access_token RENAME TO multica_personal_access_token;
ALTER TABLE verification_code RENAME TO multica_verification_code;

-- Agents & runtimes
ALTER TABLE agent RENAME TO multica_agent;
ALTER TABLE agent_runtime RENAME TO multica_agent_runtime;
ALTER TABLE daemon_connection RENAME TO multica_daemon_connection;
ALTER TABLE daemon_token RENAME TO multica_daemon_token;
ALTER TABLE squad RENAME TO multica_squad;
ALTER TABLE squad_member RENAME TO multica_squad_member;

-- Issues & projects
ALTER TABLE issue RENAME TO multica_issue;
ALTER TABLE issue_label RENAME TO multica_issue_label;
ALTER TABLE issue_to_label RENAME TO multica_issue_to_label;
ALTER TABLE issue_dependency RENAME TO multica_issue_dependency;
ALTER TABLE issue_reaction RENAME TO multica_issue_reaction;
ALTER TABLE issue_subscriber RENAME TO multica_issue_subscriber;
ALTER TABLE issue_pull_request RENAME TO multica_issue_pull_request;
ALTER TABLE project RENAME TO multica_project;
ALTER TABLE project_resource RENAME TO multica_project_resource;
ALTER TABLE comment RENAME TO multica_comment;
ALTER TABLE comment_reaction RENAME TO multica_comment_reaction;
ALTER TABLE attachment RENAME TO multica_attachment;

-- Task queue & execution
ALTER TABLE agent_task_queue RENAME TO multica_agent_task_queue;
ALTER TABLE task_usage RENAME TO multica_task_usage;
ALTER TABLE task_usage_hourly RENAME TO multica_task_usage_hourly;
ALTER TABLE task_usage_hourly_dirty RENAME TO multica_task_usage_hourly_dirty;
ALTER TABLE task_usage_hourly_rollup_state RENAME TO multica_task_usage_hourly_rollup_state;
ALTER TABLE task_message RENAME TO multica_task_message;

-- Chat
ALTER TABLE chat_session RENAME TO multica_chat_session;
ALTER TABLE chat_message RENAME TO multica_chat_message;

-- Workflows
ALTER TABLE workflow RENAME TO multica_workflow;
ALTER TABLE workflow_node RENAME TO multica_workflow_node;
ALTER TABLE workflow_edge RENAME TO multica_workflow_edge;
ALTER TABLE workflow_run RENAME TO multica_workflow_run;
ALTER TABLE workflow_node_run RENAME TO multica_workflow_node_run;

-- Autopilot
ALTER TABLE autopilot RENAME TO multica_autopilot;
ALTER TABLE autopilot_run RENAME TO multica_autopilot_run;
ALTER TABLE autopilot_trigger RENAME TO multica_autopilot_trigger;
ALTER TABLE webhook_delivery RENAME TO multica_webhook_delivery;

-- GitHub integration
ALTER TABLE github_installation RENAME TO multica_github_installation;
ALTER TABLE github_pull_request RENAME TO multica_github_pull_request;
ALTER TABLE github_pull_request_check_suite RENAME TO multica_github_pull_request_check_suite;

-- Inbox & activity
ALTER TABLE inbox_item RENAME TO multica_inbox_item;
ALTER TABLE activity_log RENAME TO multica_activity_log;

-- Skills
ALTER TABLE skill RENAME TO multica_skill;
ALTER TABLE skill_file RENAME TO multica_skill_file;
ALTER TABLE agent_skill RENAME TO multica_agent_skill;

-- Other
ALTER TABLE pinned_item RENAME TO multica_pinned_item;
ALTER TABLE feedback RENAME TO multica_feedback;
ALTER TABLE contact_sales_inquiry RENAME TO multica_contact_sales_inquiry;
ALTER TABLE notification_preference RENAME TO multica_notification_preference;
