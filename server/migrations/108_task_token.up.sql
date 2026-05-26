-- task_token replaces the historical practice of injecting the daemon
-- owner's MULTICA_TOKEN (a workspace owner/admin PAT) into the agent
-- process. That practice gave the agent full owner privileges via
-- whatever credential the daemon happened to be using, which made
-- agent-resource secrets reachable from the agent (MUL-2600).
--
-- The daemon now mints a short-lived task-scoped token at task-claim
-- time and injects THAT into the agent. The server treats `mat_`
-- tokens as authoritative for actor identity (agent + task), so a
-- request from the agent process is recognised as actor=agent
-- regardless of whether the agent strips or forges X-Agent-ID /
-- X-Task-ID headers. Owner-only endpoints (`/api/agents/{id}/env`,
-- env-management CLI) consequently 403 on agent traffic.
CREATE TABLE task_token (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash TEXT NOT NULL,
    -- Bound triple: the only identity the agent can ever claim from
    -- this token. user_id is the daemon owner (kept so member-shaped
    -- read access still resolves correctly when the agent does
    -- legitimate reads), agent_id and task_id are server-derived.
    task_id UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_task_token_hash ON task_token(token_hash);
CREATE INDEX idx_task_token_task ON task_token(task_id);
