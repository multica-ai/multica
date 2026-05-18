CREATE TABLE instructions_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    scope TEXT NOT NULL CHECK (scope IN ('personal', 'system')),
    member_id UUID REFERENCES member(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    actor_id UUID REFERENCES member(id) ON DELETE SET NULL,
    restored_from UUID REFERENCES instructions_history(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_instructions_history_scope_member CHECK (
        (scope = 'personal' AND member_id IS NOT NULL) OR
        (scope = 'system' AND member_id IS NULL)
    )
);

CREATE INDEX idx_instructions_history_lookup
    ON instructions_history(workspace_id, scope, member_id, created_at DESC);
