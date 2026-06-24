-- Runtime-level permissions for Design Two / L1.4.
-- Explicit grants (admin/operator/viewer) on top of implicit owner rights
-- from workspace owner/admin or runtime.owner_id.
CREATE TABLE multica_runtime_permission (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    runtime_id  UUID NOT NULL REFERENCES multica_agent_runtime(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES multica_user(id) ON DELETE CASCADE,
    role        TEXT NOT NULL CHECK (role IN ('admin', 'operator', 'viewer')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (runtime_id, user_id)
);

CREATE INDEX idx_runtime_permission_runtime_id ON multica_runtime_permission(runtime_id);
CREATE INDEX idx_runtime_permission_user_id ON multica_runtime_permission(user_id);
