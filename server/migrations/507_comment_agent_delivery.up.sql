-- A delivery row is the durable receipt for one comment/agent steering
-- decision. It deliberately has no foreign keys: issue, comment, task and
-- runtime deletion must not turn terminal cleanup into a cascading lock tree.
CREATE TABLE comment_agent_delivery (
    comment_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    -- Follow-up receipts have no active target task/runtime. Steering rows do.
    task_id UUID,
    runtime_id UUID,
    status TEXT NOT NULL CHECK (status IN ('pending', 'steering', 'delivered', 'follow_up')),
    failure_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    PRIMARY KEY (comment_id, agent_id)
);
