-- Multica Local: Consolidated SQLite schema
-- Combines all upstream PostgreSQL migrations into a single SQLite-compatible schema.

-- Users
CREATE TABLE IF NOT EXISTS user (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT UNIQUE NOT NULL,
    avatar_url TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Workspaces
CREATE TABLE IF NOT EXISTS workspace (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT UNIQUE NOT NULL,
    description TEXT,
    context TEXT,
    settings TEXT NOT NULL DEFAULT '{}',
    repos TEXT NOT NULL DEFAULT '[]',
    issue_prefix TEXT NOT NULL DEFAULT '',
    issue_counter INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Members (user <-> workspace)
CREATE TABLE IF NOT EXISTS member (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES user(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'admin', 'member')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(workspace_id, user_id)
);

-- Agent runtimes
CREATE TABLE IF NOT EXISTS agent_runtime (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    daemon_id TEXT,
    name TEXT NOT NULL,
    runtime_mode TEXT NOT NULL CHECK (runtime_mode IN ('local', 'cloud')),
    provider TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'offline' CHECK (status IN ('online', 'offline')),
    device_info TEXT NOT NULL DEFAULT '',
    metadata TEXT NOT NULL DEFAULT '{}',
    last_seen_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE (workspace_id, daemon_id, provider)
);

-- Agents
CREATE TABLE IF NOT EXISTS agent (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    avatar_url TEXT,
    runtime_mode TEXT NOT NULL CHECK (runtime_mode IN ('local', 'cloud')),
    runtime_config TEXT NOT NULL DEFAULT '{}',
    runtime_id TEXT NOT NULL REFERENCES agent_runtime(id) ON DELETE RESTRICT,
    visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('workspace', 'private')),
    status TEXT NOT NULL DEFAULT 'offline' CHECK (status IN ('idle', 'working', 'blocked', 'error', 'offline')),
    max_concurrent_tasks INTEGER NOT NULL DEFAULT 6,
    owner_id TEXT REFERENCES user(id),
    tools TEXT NOT NULL DEFAULT '[]',
    triggers TEXT NOT NULL DEFAULT '[]',
    instructions TEXT NOT NULL DEFAULT '',
    archived_at TEXT DEFAULT NULL,
    archived_by TEXT DEFAULT NULL REFERENCES user(id),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Issues
CREATE TABLE IF NOT EXISTS issue (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'backlog'
        CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled')),
    priority TEXT NOT NULL DEFAULT 'none'
        CHECK (priority IN ('urgent', 'high', 'medium', 'low', 'none')),
    assignee_type TEXT CHECK (assignee_type IN ('member', 'agent')),
    assignee_id TEXT,
    creator_type TEXT NOT NULL CHECK (creator_type IN ('member', 'agent')),
    creator_id TEXT NOT NULL,
    parent_issue_id TEXT REFERENCES issue(id) ON DELETE SET NULL,
    acceptance_criteria TEXT NOT NULL DEFAULT '[]',
    context_refs TEXT NOT NULL DEFAULT '[]',
    position REAL NOT NULL DEFAULT 0,
    due_date TEXT,
    number INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(workspace_id, number)
);

-- Issue labels
CREATE TABLE IF NOT EXISTS issue_label (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    color TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS issue_to_label (
    issue_id TEXT NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    label_id TEXT NOT NULL REFERENCES issue_label(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, label_id)
);

-- Issue dependencies
CREATE TABLE IF NOT EXISTS issue_dependency (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    depends_on_issue_id TEXT NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('blocks', 'blocked_by', 'related'))
);

-- Comments
CREATE TABLE IF NOT EXISTS comment (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    author_type TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
    author_id TEXT NOT NULL,
    content TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'comment'
        CHECK (type IN ('comment', 'status_change', 'progress_update', 'system')),
    parent_id TEXT REFERENCES comment(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Inbox items
CREATE TABLE IF NOT EXISTS inbox_item (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    recipient_type TEXT NOT NULL CHECK (recipient_type IN ('member', 'agent')),
    recipient_id TEXT NOT NULL,
    type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info'
        CHECK (severity IN ('action_required', 'attention', 'info')),
    issue_id TEXT REFERENCES issue(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    body TEXT,
    read INTEGER NOT NULL DEFAULT 0,
    archived INTEGER NOT NULL DEFAULT 0,
    actor_type TEXT,
    actor_id TEXT,
    details TEXT DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Agent task queue
CREATE TABLE IF NOT EXISTS agent_task_queue (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    runtime_id TEXT NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    issue_id TEXT NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'dispatched', 'running', 'completed', 'failed', 'cancelled')),
    priority INTEGER NOT NULL DEFAULT 0,
    context TEXT,
    session_id TEXT,
    work_dir TEXT,
    trigger_comment_id TEXT REFERENCES comment(id) ON DELETE SET NULL,
    dispatched_at TEXT,
    started_at TEXT,
    completed_at TEXT,
    result TEXT,
    error TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Activity log
CREATE TABLE IF NOT EXISTS activity_log (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id TEXT REFERENCES issue(id) ON DELETE CASCADE,
    actor_type TEXT CHECK (actor_type IN ('member', 'agent', 'system')),
    actor_id TEXT,
    action TEXT NOT NULL,
    details TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Verification codes
CREATE TABLE IF NOT EXISTS verification_code (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    code TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used INTEGER NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Personal access tokens
CREATE TABLE IF NOT EXISTS personal_access_token (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES user(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL,
    token_prefix TEXT NOT NULL,
    expires_at TEXT,
    last_used_at TEXT,
    revoked INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Runtime usage
CREATE TABLE IF NOT EXISTS runtime_usage (
    id TEXT PRIMARY KEY,
    runtime_id TEXT NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    date TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE (runtime_id, date, provider, model)
);

-- Issue subscribers
CREATE TABLE IF NOT EXISTS issue_subscriber (
    issue_id TEXT NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    user_type TEXT NOT NULL CHECK (user_type IN ('member', 'agent')),
    user_id TEXT NOT NULL,
    reason TEXT NOT NULL CHECK (reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (issue_id, user_type, user_id)
);

-- Comment reactions
CREATE TABLE IF NOT EXISTS comment_reaction (
    id TEXT PRIMARY KEY,
    comment_id TEXT NOT NULL REFERENCES comment(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent')),
    actor_id TEXT NOT NULL,
    emoji TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE (comment_id, actor_type, actor_id, emoji)
);

-- Task messages
CREATE TABLE IF NOT EXISTS task_message (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    seq INTEGER NOT NULL,
    type TEXT NOT NULL,
    tool TEXT,
    content TEXT,
    input TEXT,
    output TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Issue reactions
CREATE TABLE IF NOT EXISTS issue_reaction (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    workspace_id TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent')),
    actor_id TEXT NOT NULL,
    emoji TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE (issue_id, actor_type, actor_id, emoji)
);

-- Attachments
CREATE TABLE IF NOT EXISTS attachment (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id TEXT REFERENCES issue(id) ON DELETE CASCADE,
    comment_id TEXT REFERENCES comment(id) ON DELETE CASCADE,
    uploader_type TEXT NOT NULL CHECK (uploader_type IN ('member', 'agent')),
    uploader_id TEXT NOT NULL,
    filename TEXT NOT NULL,
    url TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Daemon tokens
CREATE TABLE IF NOT EXISTS daemon_token (
    id TEXT PRIMARY KEY,
    token_hash TEXT NOT NULL,
    workspace_id TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    daemon_id TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

-- Skills
CREATE TABLE IF NOT EXISTS skill (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    config TEXT NOT NULL DEFAULT '{}',
    created_by TEXT REFERENCES user(id),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(workspace_id, name)
);

CREATE TABLE IF NOT EXISTS skill_file (
    id TEXT PRIMARY KEY,
    skill_id TEXT NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(skill_id, path)
);

CREATE TABLE IF NOT EXISTS agent_skill (
    agent_id TEXT NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    skill_id TEXT NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    PRIMARY KEY (agent_id, skill_id)
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_issue_workspace ON issue(workspace_id);
CREATE INDEX IF NOT EXISTS idx_issue_assignee ON issue(assignee_type, assignee_id);
CREATE INDEX IF NOT EXISTS idx_issue_status ON issue(workspace_id, status);
CREATE INDEX IF NOT EXISTS idx_issue_parent ON issue(parent_issue_id);
CREATE INDEX IF NOT EXISTS idx_issue_workspace_number ON issue(workspace_id, number);
CREATE INDEX IF NOT EXISTS idx_comment_issue ON comment(issue_id);
CREATE INDEX IF NOT EXISTS idx_inbox_recipient ON inbox_item(recipient_type, recipient_id, read);
CREATE INDEX IF NOT EXISTS idx_agent_task_queue_agent ON agent_task_queue(agent_id, status);
CREATE INDEX IF NOT EXISTS idx_agent_task_queue_runtime_pending ON agent_task_queue(runtime_id, priority, created_at);
CREATE INDEX IF NOT EXISTS idx_activity_log_issue ON activity_log(issue_id);
CREATE INDEX IF NOT EXISTS idx_member_workspace ON member(workspace_id);
CREATE INDEX IF NOT EXISTS idx_agent_workspace ON agent(workspace_id);
CREATE INDEX IF NOT EXISTS idx_agent_runtime_workspace ON agent_runtime(workspace_id);
CREATE INDEX IF NOT EXISTS idx_agent_runtime_status ON agent_runtime(workspace_id, status);
CREATE INDEX IF NOT EXISTS idx_verification_code_email ON verification_code(email, used, expires_at);
CREATE INDEX IF NOT EXISTS idx_pat_user ON personal_access_token(user_id, revoked);
CREATE UNIQUE INDEX IF NOT EXISTS idx_pat_token_hash ON personal_access_token(token_hash);
CREATE INDEX IF NOT EXISTS idx_runtime_usage_runtime_date ON runtime_usage(runtime_id, date);
CREATE INDEX IF NOT EXISTS idx_issue_subscriber_user ON issue_subscriber(user_type, user_id);
CREATE INDEX IF NOT EXISTS idx_comment_reaction_comment_id ON comment_reaction(comment_id);
CREATE INDEX IF NOT EXISTS idx_task_message_task_id_seq ON task_message(task_id, seq);
CREATE INDEX IF NOT EXISTS idx_issue_reaction_issue_id ON issue_reaction(issue_id);
CREATE INDEX IF NOT EXISTS idx_attachment_issue ON attachment(issue_id);
CREATE INDEX IF NOT EXISTS idx_attachment_comment ON attachment(comment_id);
CREATE INDEX IF NOT EXISTS idx_attachment_workspace ON attachment(workspace_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_daemon_token_hash ON daemon_token(token_hash);
CREATE INDEX IF NOT EXISTS idx_daemon_token_workspace_daemon ON daemon_token(workspace_id, daemon_id);
CREATE INDEX IF NOT EXISTS idx_skill_workspace ON skill(workspace_id);
CREATE INDEX IF NOT EXISTS idx_skill_file_skill ON skill_file(skill_id);
CREATE INDEX IF NOT EXISTS idx_agent_skill_skill ON agent_skill(skill_id);
CREATE INDEX IF NOT EXISTS idx_agent_skill_agent ON agent_skill(agent_id);

-- Schema migrations tracking
CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
