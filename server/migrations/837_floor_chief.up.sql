-- The floor chief of an office floor (spec 4.8): an agent, usually from the
-- floor_chief template, that publishes the floor's daily brief. One chief per
-- floor (see 838). No foreign keys: the floor and the agent are validated in
-- application code, and an archived agent is refused when the chief is set.
CREATE TABLE IF NOT EXISTS floor_chief (
    workspace_id UUID NOT NULL,
    floor_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
