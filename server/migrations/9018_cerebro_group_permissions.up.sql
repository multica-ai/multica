-- Cerebro groups permission model (JEH-1008, PR 2 of JEH-1006).
--
-- Four whitelist tables keyed on cerebro_group:
--
--   cerebro_group_capability      — toggles ('create_runtime', 'create_agent')
--   cerebro_group_runtime_access  — allowlist: runtimes a group may use
--   cerebro_group_agent_access    — allowlist: agents a group may trigger
--   cerebro_project_group_member  — group-level project access (OR-led with
--                                   the existing project_member user rows)
--
-- Plus a per-workspace default-group pointer so application code can find
-- the workspace's "All members" group on workspace-member invite without
-- changing the cerebro_group table shape (the existing SELECT/RETURNING
-- column lists already pin the row struct).
--
-- Resolution pattern matches autopilot-scope: callers resolve "viewer's
-- effective group-ids", workspace owner/admin always overrides. Member with
-- no group → deny by default. Until PR 3 lands enforcement, none of this
-- changes runtime behavior; PR 2 just persists the rules.
--
-- Backfill: every workspace gets a default 'All members' group with all
-- existing members, all existing runtimes/agents on its allowlists, both
-- create_* capabilities, and access to every existing project. That keeps
-- the deny-by-default policy from breaking workflows that already work.
--
-- Idempotent: every statement uses IF NOT EXISTS / ON CONFLICT so that
-- TestMigrationsAreIdempotent's re-run pass is a no-op.

CREATE TABLE IF NOT EXISTS cerebro_group_capability (
    group_id   UUID NOT NULL REFERENCES cerebro_group(id) ON DELETE CASCADE,
    capability TEXT NOT NULL,
    granted_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, capability),
    CONSTRAINT cerebro_group_capability_known CHECK (
        capability IN ('create_runtime', 'create_agent')
    )
);

CREATE INDEX IF NOT EXISTS idx_cerebro_group_capability_capability
    ON cerebro_group_capability (capability);

CREATE TABLE IF NOT EXISTS cerebro_group_runtime_access (
    group_id   UUID NOT NULL REFERENCES cerebro_group(id) ON DELETE CASCADE,
    runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    granted_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, runtime_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_group_runtime_access_runtime
    ON cerebro_group_runtime_access (runtime_id);

CREATE TABLE IF NOT EXISTS cerebro_group_agent_access (
    group_id   UUID NOT NULL REFERENCES cerebro_group(id) ON DELETE CASCADE,
    agent_id   UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    granted_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_group_agent_access_agent
    ON cerebro_group_agent_access (agent_id);

CREATE TABLE IF NOT EXISTS cerebro_project_group_member (
    project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    group_id   UUID NOT NULL REFERENCES cerebro_group(id) ON DELETE CASCADE,
    added_by   UUID REFERENCES "user"(id) ON DELETE SET NULL,
    added_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_project_group_member_group
    ON cerebro_project_group_member (group_id);

-- Per-workspace pointer to the default 'All members' group. Separate table
-- so cerebro_group's struct shape stays untouched (sqlc-generated SELECTs
-- and RETURNINGs in group.sql pin the column list explicitly).
CREATE TABLE IF NOT EXISTS cerebro_workspace_default_group (
    workspace_id UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    group_id     UUID NOT NULL REFERENCES cerebro_group(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Backfill: one default group per workspace, populated with every existing
-- member + every existing runtime/agent/project. The "All members" name is
-- a default the user can rename in the UI; service-layer code only relies
-- on cerebro_workspace_default_group.group_id to find it.

-- Re-running up after migrate-down would normally duplicate 'All members'
-- groups (the down only drops the new tables; the cerebro_group rows survive).
-- Guard with both checks so a down/up cycle re-links the existing group rather
-- than creating a second one with the same name.
INSERT INTO cerebro_group (workspace_id, name, description)
SELECT w.id,
       'All members',
       'Default group created automatically; every workspace member belongs here.'
FROM workspace w
WHERE NOT EXISTS (
    SELECT 1 FROM cerebro_workspace_default_group d
    WHERE d.workspace_id = w.id
)
AND NOT EXISTS (
    SELECT 1 FROM cerebro_group g
    WHERE g.workspace_id = w.id AND g.name = 'All members'
);

INSERT INTO cerebro_workspace_default_group (workspace_id, group_id)
SELECT DISTINCT ON (g.workspace_id) g.workspace_id, g.id
FROM cerebro_group g
WHERE g.name = 'All members'
  AND NOT EXISTS (
      SELECT 1 FROM cerebro_workspace_default_group d
      WHERE d.workspace_id = g.workspace_id
  )
ORDER BY g.workspace_id, g.created_at ASC;

INSERT INTO cerebro_group_member (group_id, user_id)
SELECT d.group_id, m.user_id
FROM cerebro_workspace_default_group d
JOIN member m ON m.workspace_id = d.workspace_id
ON CONFLICT (group_id, user_id) DO NOTHING;

INSERT INTO cerebro_group_capability (group_id, capability)
SELECT d.group_id, c.capability
FROM cerebro_workspace_default_group d
CROSS JOIN (VALUES ('create_runtime'), ('create_agent')) AS c(capability)
ON CONFLICT (group_id, capability) DO NOTHING;

INSERT INTO cerebro_group_runtime_access (group_id, runtime_id)
SELECT d.group_id, rt.id
FROM cerebro_workspace_default_group d
JOIN agent_runtime rt ON rt.workspace_id = d.workspace_id
ON CONFLICT (group_id, runtime_id) DO NOTHING;

INSERT INTO cerebro_group_agent_access (group_id, agent_id)
SELECT d.group_id, a.id
FROM cerebro_workspace_default_group d
JOIN agent a ON a.workspace_id = d.workspace_id
ON CONFLICT (group_id, agent_id) DO NOTHING;

INSERT INTO cerebro_project_group_member (project_id, group_id)
SELECT p.id, d.group_id
FROM cerebro_workspace_default_group d
JOIN project p ON p.workspace_id = d.workspace_id
ON CONFLICT (project_id, group_id) DO NOTHING;
