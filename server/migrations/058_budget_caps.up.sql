-- Budget cap kill-switch (JEH-327).
--
-- Two independent axes (per-user, per-agent) plus a workspace-level safety
-- line. All caps are NULL by default — the workspace owner must complete the
-- setup wizard before any cap is set, and until then `workspace_budget_config`
-- has no row at all (or `configured_at IS NULL`), which the server treats as
-- "no claims allowed" once the pre-claim check lands in a follow-up.

CREATE TABLE workspace_budget_config (
    workspace_id                       UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,

    -- Per-scope defaults; NULL means "no cap enforced for this scope".
    default_user_daily_cap_cents       BIGINT,
    default_user_monthly_cap_cents     BIGINT,
    default_agent_daily_cap_cents      BIGINT,
    default_agent_monthly_cap_cents    BIGINT,

    -- Workspace-wide safety line.
    workspace_daily_alarm_cents        BIGINT,
    workspace_daily_hard_kill_cents    BIGINT,

    -- Audit.
    configured_at                      TIMESTAMPTZ,
    configured_by                      UUID REFERENCES "user"(id),
    last_modified_at                   TIMESTAMPTZ,
    last_modified_by                   UUID REFERENCES "user"(id),

    CHECK (default_user_daily_cap_cents     IS NULL OR default_user_daily_cap_cents     >= 0),
    CHECK (default_user_monthly_cap_cents   IS NULL OR default_user_monthly_cap_cents   >= 0),
    CHECK (default_agent_daily_cap_cents    IS NULL OR default_agent_daily_cap_cents    >= 0),
    CHECK (default_agent_monthly_cap_cents  IS NULL OR default_agent_monthly_cap_cents  >= 0),
    CHECK (workspace_daily_alarm_cents      IS NULL OR workspace_daily_alarm_cents      >= 0),
    CHECK (workspace_daily_hard_kill_cents  IS NULL OR workspace_daily_hard_kill_cents  >= 0)
);

-- Per-user override; NULL field = inherit workspace default.
CREATE TABLE user_budget_override (
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id             UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    daily_cap_cents     BIGINT,
    monthly_cap_cents   BIGINT,
    reason              TEXT,
    set_by              UUID NOT NULL REFERENCES "user"(id),
    set_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id),
    CHECK (daily_cap_cents   IS NULL OR daily_cap_cents   >= 0),
    CHECK (monthly_cap_cents IS NULL OR monthly_cap_cents >= 0)
);

-- Per-agent override.
CREATE TABLE agent_budget_override (
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id            UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    daily_cap_cents     BIGINT,
    monthly_cap_cents   BIGINT,
    reason              TEXT,
    set_by              UUID NOT NULL REFERENCES "user"(id),
    set_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, agent_id),
    CHECK (daily_cap_cents   IS NULL OR daily_cap_cents   >= 0),
    CHECK (monthly_cap_cents IS NULL OR monthly_cap_cents >= 0)
);

-- Live spend per (scope, window). One row per (workspace, scope, scope_id, window).
-- scope_id is polymorphic — user_id, agent_id, or the workspace_id itself when
-- scope_type = 'workspace'. window_start is the first day of the window
-- (always day-precision; monthly windows use the 1st).
CREATE TABLE budget_state (
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    scope_type      TEXT NOT NULL CHECK (scope_type IN ('user','agent','workspace')),
    scope_id        UUID NOT NULL,
    window_type     TEXT NOT NULL CHECK (window_type IN ('day','month')),
    window_start    DATE NOT NULL,
    cents_spent     BIGINT NOT NULL DEFAULT 0 CHECK (cents_spent >= 0),
    last_updated    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, scope_type, scope_id, window_type, window_start)
);
CREATE INDEX idx_budget_state_window ON budget_state (workspace_id, window_type, window_start);

-- Audit log of every cap change. Captures both setup-wizard writes and
-- per-(user|agent) override edits.
CREATE TABLE budget_change_log (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    changed_by      UUID NOT NULL REFERENCES "user"(id),
    changed_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    scope_type      TEXT NOT NULL CHECK (scope_type IN ('workspace_default','user_override','agent_override','workspace_safety')),
    scope_id        UUID,
    field           TEXT NOT NULL,
    old_value_cents BIGINT,
    new_value_cents BIGINT,
    reason          TEXT
);
CREATE INDEX idx_budget_change_log_workspace ON budget_change_log (workspace_id, changed_at DESC);
