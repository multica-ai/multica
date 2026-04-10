-- Global skills reported by a daemon runtime (e.g. files in ~/.claude/skills/).
-- Read-only from the server's perspective — the daemon owns writes via registration.

CREATE TABLE runtime_global_skill (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    runtime_id  UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(runtime_id, name)
);

CREATE INDEX idx_runtime_global_skill_runtime ON runtime_global_skill(runtime_id);
