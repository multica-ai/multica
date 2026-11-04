-- MUL-8516: Connected Apps v2 (workspace team accounts, personal accounts,
-- per-agent levels, per-action rules, confirmation requests, activity).
--
-- A personal account stays in user_composio_connection (one row per user,
-- usable in every workspace the user belongs to). A team account is new: one
-- Composio connected account owned by the workspace, created by an admin, used
-- by every agent the workspace adds it to. Its composio_user_id is
-- "ws_<workspace_id>" so a team account never shares a Composio user with a
-- person.
--
-- No foreign keys (repo rule): workspace and agent teardown delete these rows
-- explicitly. Every index is built concurrently in 612-621, following
-- wecom_install_session (600-604), so no migration builds an index inline on a
-- live table. The tables start empty, so the window between this migration
-- and its indexes carries no load.
CREATE TABLE IF NOT EXISTS workspace_composio_connection (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL,
    toolkit_slug         TEXT NOT NULL,
    auth_config_id       TEXT NOT NULL,
    connected_account_id TEXT NOT NULL,
    composio_user_id     TEXT NOT NULL,
    connected_by         UUID NOT NULL,
    status               TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'expired', 'revoked')),
    connected_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Which apps an agent may use, through which kind of account, at which level.
--   account_scope: team     = the workspace's team account
--                  personal = the account of whoever triggered the run
--   level:         read           = read tools only
--                  write          = everything the action rules allow
--                  confirm_writes = every non-read call needs confirmation
CREATE TABLE IF NOT EXISTS agent_app (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL,
    agent_id      UUID NOT NULL,
    toolkit_slug  TEXT NOT NULL,
    account_scope TEXT NOT NULL CHECK (account_scope IN ('team', 'personal')),
    level         TEXT NOT NULL DEFAULT 'confirm_writes'
        CHECK (level IN ('read', 'write', 'confirm_writes')),
    created_by    UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Per-action rules. owner_user_id NULL is the workspace's rule for the team
-- account; a user id is that member's own rule for their personal account in
-- this workspace. A missing row means the built-in default for the class.
CREATE TABLE IF NOT EXISTS app_action_rule (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL,
    owner_user_id UUID,
    toolkit_slug  TEXT NOT NULL,
    action_class  TEXT NOT NULL
        CHECK (action_class IN ('read', 'write', 'send', 'delete', 'share')),
    policy        TEXT NOT NULL CHECK (policy IN ('allow', 'confirm', 'block')),
    updated_by    UUID,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A call an agent made that waits on a person: an action that needs
-- confirmation, or an app the person still has to connect. The arguments are
-- what the agent asked for; approved_arguments is what the approver accepted
-- (equal unless they edited it) and is exactly what Multica executes.
CREATE TABLE IF NOT EXISTS app_action_request (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id       UUID NOT NULL,
    kind               TEXT NOT NULL DEFAULT 'action' CHECK (kind IN ('action', 'connect')),
    status             TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'failed', 'cancelled')),
    task_id            UUID NOT NULL,
    agent_id           UUID NOT NULL,
    issue_id           UUID,
    chat_session_id    UUID,
    approver_user_id   UUID NOT NULL,
    account_scope      TEXT NOT NULL CHECK (account_scope IN ('team', 'personal')),
    toolkit_slug       TEXT NOT NULL,
    tool_slug          TEXT NOT NULL DEFAULT '',
    action_class       TEXT NOT NULL DEFAULT 'send',
    arguments          JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- One sentence from the agent: why it wants to make this call.
    summary            TEXT NOT NULL DEFAULT '',
    approved_arguments JSONB,
    result             JSONB,
    error              TEXT,
    reason             TEXT,
    expires_at         TIMESTAMPTZ NOT NULL,
    reminded_at        TIMESTAMPTZ,
    decided_by         UUID,
    decided_at         TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per tool call that reached the gateway, whatever its outcome. Backs
-- the activity list on app pages. Arguments are not stored here.
CREATE TABLE IF NOT EXISTS app_tool_call (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          UUID NOT NULL,
    task_id               UUID,
    agent_id              UUID NOT NULL,
    issue_id              UUID,
    chat_session_id       UUID,
    triggered_by          UUID,
    account_scope         TEXT NOT NULL,
    account_owner_user_id UUID,
    toolkit_slug          TEXT NOT NULL,
    tool_slug             TEXT NOT NULL,
    action_class          TEXT NOT NULL,
    outcome               TEXT NOT NULL
        CHECK (outcome IN ('ok', 'error', 'blocked', 'pending', 'approved', 'rejected', 'rate_limited')),
    request_id            UUID,
    error                 TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Which apps members may connect. 'all' is every app in the catalog,
-- 'curated' only the curated ones, 'custom' only allowed_toolkits. No row
-- means 'all'.
CREATE TABLE IF NOT EXISTS workspace_app_setting (
    workspace_id     UUID PRIMARY KEY,
    catalog_mode     TEXT NOT NULL DEFAULT 'all' CHECK (catalog_mode IN ('all', 'curated', 'custom')),
    allowed_toolkits TEXT[] NOT NULL DEFAULT '{}',
    updated_by       UUID,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- An admin's correction of how one tool is classified in this workspace. It
-- wins over the curated table and the name heuristics.
CREATE TABLE IF NOT EXISTS app_tool_class_override (
    workspace_id UUID NOT NULL,
    toolkit_slug TEXT NOT NULL,
    tool_slug    TEXT NOT NULL,
    action_class TEXT NOT NULL
        CHECK (action_class IN ('read', 'write', 'send', 'delete', 'share')),
    updated_by   UUID,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, toolkit_slug, tool_slug)
);

-- Carry over agents that allowlisted toolkits under the first version. That
-- version mounted the agent owner's own accounts; the closest equivalent is a
-- personal app, and confirm_writes keeps every write behind a confirmation
-- until the owner chooses otherwise. The feature flag never shipped, so this
-- touches only internal workspaces. DISTINCT guards duplicate slugs in a row.
INSERT INTO agent_app (workspace_id, agent_id, toolkit_slug, account_scope, level, created_by)
SELECT DISTINCT a.workspace_id, a.id, lower(btrim(slug)), 'personal', 'confirm_writes', a.owner_id
FROM agent a, unnest(a.composio_toolkit_allowlist) AS slug
WHERE a.composio_toolkit_allowlist IS NOT NULL
  AND cardinality(a.composio_toolkit_allowlist) > 0
  AND btrim(slug) <> '';
