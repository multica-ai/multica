-- Initiative event: append-only audit trail of every orchestrator decision,
-- state transition, and human action on an initiative. Never updated or
-- deleted by the application; cancelling an initiative closes child rows in
-- other tables but leaves the event history intact. `actor_id` is NULL for
-- actor_type='system'. Payload carries the event-type-specific detail
-- (LLM reasoning summaries, transition from/to, usage snapshots, ...).
CREATE TABLE initiative_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    initiative_id UUID NOT NULL,
    task_id UUID,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent', 'system')),
    actor_id UUID,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
