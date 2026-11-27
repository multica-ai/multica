-- Consent is bound to a specific owner. Transferring the agent does not
-- silently transfer account-wide authority to its new owner.
CREATE TABLE agent_full_delegation (
    agent_id UUID PRIMARY KEY,
    grantor_user_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
