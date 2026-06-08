-- Workflow Orchestration Engine: 5 new tables + agent_task_queue alter.
-- Each workflow is a DAG of nodes connected by directed edges.
-- A workflow_run is one execution instance; workflow_node_run tracks
-- each node's progress through a 3-phase (Format→Worker→Critic) state machine.

-- workflow: top-level DAG definition
CREATE TABLE workflow (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'paused', 'archived')),
    max_retries INT NOT NULL DEFAULT 3 CHECK (max_retries >= 0 AND max_retries <= 10),
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_workflow_workspace_id ON workflow(workspace_id);

-- workflow_node: one node in the DAG
CREATE TABLE workflow_node (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    position_x FLOAT NOT NULL DEFAULT 0,
    position_y FLOAT NOT NULL DEFAULT 0,
    -- Format checker: JSON Schema string (empty = no format validation)
    format_schema JSONB DEFAULT NULL,
    -- Worker phase config
    worker_type TEXT NOT NULL CHECK (worker_type IN ('human', 'agent', 'squad')),
    worker_id UUID DEFAULT NULL,
    -- Critic phase config
    critic_type TEXT NOT NULL CHECK (critic_type IN ('human', 'agent', 'squad', 'api')),
    critic_id UUID DEFAULT NULL,
    critic_api_url TEXT DEFAULT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_workflow_node_workflow_id ON workflow_node(workflow_id);

-- workflow_edge: directed edge between two nodes
CREATE TABLE workflow_edge (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    source_node_id UUID NOT NULL REFERENCES workflow_node(id) ON DELETE CASCADE,
    target_node_id UUID NOT NULL REFERENCES workflow_node(id) ON DELETE CASCADE,
    condition JSONB DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workflow_id, source_node_id, target_node_id),
    CHECK (source_node_id != target_node_id)
);
CREATE INDEX idx_workflow_edge_workflow_id ON workflow_edge(workflow_id);
CREATE INDEX idx_workflow_edge_source ON workflow_edge(source_node_id);
CREATE INDEX idx_workflow_edge_target ON workflow_edge(target_node_id);

-- workflow_run: one execution instance of a workflow
CREATE TABLE workflow_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    workflow_title TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    triggered_by_type TEXT NOT NULL CHECK (triggered_by_type IN ('member', 'agent', 'autopilot', 'api')),
    triggered_by_id UUID DEFAULT NULL,
    input JSONB DEFAULT '{}',
    output JSONB DEFAULT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_workflow_run_workflow_id ON workflow_run(workflow_id);
CREATE INDEX idx_workflow_run_workspace_id ON workflow_run(workspace_id);

-- workflow_node_run: one node execution within a run
CREATE TABLE workflow_node_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_run_id UUID NOT NULL REFERENCES workflow_run(id) ON DELETE CASCADE,
    workflow_node_id UUID NOT NULL REFERENCES workflow_node(id) ON DELETE CASCADE,
    node_title TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN (
        'pending',
        'format_checking',
        'format_ok',
        'format_failed',
        'worker_assigned',
        'working',
        'awaiting_critic',
        'critic_reviewing',
        'critic_approved',
        'completed',
        'failed',
        'blocked',
        'skipped',
        'cancelled'
    )),
    retry_count INT NOT NULL DEFAULT 0,
    worker_type TEXT NOT NULL,
    worker_id UUID DEFAULT NULL,
    worker_output JSONB DEFAULT NULL,
    critic_type TEXT NOT NULL,
    critic_id UUID DEFAULT NULL,
    critic_output JSONB DEFAULT NULL,
    critic_comment TEXT DEFAULT '',
    agent_task_id UUID DEFAULT NULL,
    started_at TIMESTAMPTZ DEFAULT NULL,
    completed_at TIMESTAMPTZ DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_workflow_node_run_workflow_run_id ON workflow_node_run(workflow_run_id);
CREATE INDEX idx_workflow_node_run_workflow_node_id ON workflow_node_run(workflow_node_id);
CREATE INDEX idx_workflow_node_run_status ON workflow_node_run(workflow_run_id, status);

-- Link agent tasks to workflow node runs (nullable — NULL for non-workflow tasks)
ALTER TABLE agent_task_queue ADD COLUMN workflow_node_run_id UUID REFERENCES workflow_node_run(id) ON DELETE SET NULL;
