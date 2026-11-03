-- Who may read or write a restricted knowledge base (plan 10.13). A grant names
-- a squad, a floor or one agent; the agents it covers inherit it. There is no
-- primary key and no foreign key: a grant is identified by its base and subject
-- (the unique index in 762), and the workspace teardown sweeps the table.
CREATE TABLE knowledge_acl (
    workspace_id UUID NOT NULL,
    knowledge_base_id UUID NOT NULL,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('squad', 'floor', 'agent')),
    subject_id UUID NOT NULL,
    access TEXT NOT NULL CHECK (access IN ('read', 'write')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
