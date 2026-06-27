-- Custom Runtime, PR1 (schema only). See MUL-3284 / GitHub issue #3667.
--
-- Adds the workspace-level `runtime_profile` table (the shared, team-visible
-- definition of a "custom runtime" — e.g. an in-house Codex wrapper) and gives
-- `agent_runtime` a stable `profile_id` so the same daemon can host multiple
-- runtimes of the same protocol family.
--
-- Referential integrity policy (house rule): this migration does NOT add any
-- new database foreign keys or ON DELETE cascades. `workspace_id`,
-- `created_by` and `agent_runtime.profile_id` are plain UUID columns; the
-- relationships they model are enforced in the application layer, not by the
-- database. In particular, deleting a runtime_profile must clean up its
-- associated agent_runtime instance rows in application code (PR2's profile
-- delete path) — the database will no longer cascade that for us.
--
-- Scope is deliberately additive only:
--   * The legacy `UNIQUE (workspace_id, daemon_id, provider)` constraint on
--     agent_runtime is left INTACT so the existing registration upsert
--     (`ON CONFLICT (workspace_id, daemon_id, provider)` in runtime.sql) keeps
--     resolving its arbiter. Converting that key into a partial index
--     (WHERE profile_id IS NULL) and teaching the upsert to be profile-aware
--     is PR2's registration work, not this migration's.
--   * `profile_id` is NULL for every existing/built-in runtime row, so the new
--     partial unique index does not constrain any current data.
--
-- Iron rule honored here at the schema level: the profile does NOT carry a
-- generic per-agent args field. Per-agent launch args continue to live on
-- `agent.custom_args`. The only args column is `fixed_args` — the fixed
-- arguments that EVERY agent on this runtime must inherit to enter a
-- compatible mode (advanced/optional, defaults to an empty array).

CREATE TABLE runtime_profile (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Owning workspace. Plain UUID; integrity (and cleanup on workspace
    -- delete) is enforced in the application layer, not by a DB FK.
    workspace_id UUID NOT NULL,
    display_name TEXT NOT NULL,
    -- protocol_family must stay in lockstep with the agent.New() switch in
    -- server/pkg/agent/agent.go. A profile may only be based on a backend
    -- Multica already officially supports and tests.
    protocol_family TEXT NOT NULL CHECK (protocol_family IN (
        'claude',
        'codebuddy',
        'codex',
        'copilot',
        'opencode',
        'openclaw',
        'hermes',
        'gemini',
        'pi',
        'cursor',
        'kimi',
        'kiro',
        'antigravity'
    )),
    command_name TEXT NOT NULL,
    description TEXT,
    fixed_args JSONB NOT NULL DEFAULT '[]',
    visibility TEXT NOT NULL DEFAULT 'workspace' CHECK (visibility IN ('workspace', 'private')),
    -- Creating user. Plain UUID, nullable; no DB FK.
    created_by UUID,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, display_name)
);

CREATE INDEX idx_runtime_profile_workspace ON runtime_profile(workspace_id);

-- Stable profile identity on the runtime instance row. NULL = built-in runtime
-- (registered the legacy way); non-NULL = a registered instance of a custom
-- profile. Plain UUID with no DB FK: the link to runtime_profile, and the
-- cleanup of these rows when a profile is deleted, is the application layer's
-- responsibility (PR2).
ALTER TABLE agent_runtime
    ADD COLUMN profile_id UUID;

-- Custom-runtime uniqueness: one instance per (workspace, daemon, profile).
-- Partial so it never touches built-in rows (profile_id IS NULL) and never
-- conflicts with the legacy (workspace_id, daemon_id, provider) constraint.
CREATE UNIQUE INDEX agent_runtime_workspace_daemon_profile_key
    ON agent_runtime (workspace_id, daemon_id, profile_id)
    WHERE profile_id IS NOT NULL;
