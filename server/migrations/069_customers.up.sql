CREATE TABLE customer (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    website TEXT,
    email TEXT,
    phone TEXT,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_customer_workspace ON customer(workspace_id);
CREATE INDEX idx_customer_workspace_name ON customer(workspace_id, lower(name));

ALTER TABLE project
    ADD COLUMN customer_id UUID REFERENCES customer(id) ON DELETE SET NULL;

CREATE INDEX idx_project_customer ON project(customer_id);
