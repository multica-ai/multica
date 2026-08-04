-- Immutable configurations. Updating a hook creates a new revision row.
CREATE TABLE hook_revision (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hook_id UUID NOT NULL,
    revision INT NOT NULL,
    event_type TEXT NOT NULL,
    match JSONB NOT NULL DEFAULT '{}'::jsonb,
    conditions JSONB,
    fire_mode TEXT NOT NULL CHECK (fire_mode IN ('per_event', 'rising_edge')),
    actions JSONB NOT NULL,
    created_by_type TEXT NOT NULL,
    created_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

COMMENT ON TABLE hook_revision IS 'Immutable Event Hooks configuration revisions. No foreign keys; application validates hook ownership.';
