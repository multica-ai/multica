-- Accepted comment inputs outlive the task currently occupying their slot.
-- Relationships are application-managed; indexes are built in 686 and 687.
CREATE TABLE IF NOT EXISTS comment_delivery_obligation (
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    comment_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    comment_revision BIGINT NOT NULL,
    comment_content TEXT NOT NULL,
    input_updated_at TIMESTAMPTZ NOT NULL,
    actor_type TEXT NOT NULL,
    actor_id UUID,
    originator_user_id UUID,
    authoring_task_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_until TIMESTAMPTZ NOT NULL DEFAULT '-infinity'
);
