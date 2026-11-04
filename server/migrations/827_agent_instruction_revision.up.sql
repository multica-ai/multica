-- Every change to an agent's instructions, from any path, as one row (spec 4.6).
-- revision counts up per agent. source is human (an edit), proposal (an applied
-- org_change) or rollback (a restore, which is itself a new revision). The row
-- is written in the same transaction as the change it records.
-- No foreign keys: the agent and proposal are checked by the writer.
CREATE TABLE IF NOT EXISTS agent_instruction_revision (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    instructions TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('human', 'proposal', 'rollback')),
    proposal_id UUID,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent', 'system')),
    actor_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
