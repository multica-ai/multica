-- Plan: PRD 确认后的快照
CREATE TABLE plan (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    creator_id UUID NOT NULL REFERENCES member(id),
    title TEXT NOT NULL,
    content TEXT,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'confirmed', 'running', 'done', 'cancelled')),
    workflow_id UUID,  -- backfilled after workflow creation; nullable until then
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_plan_workspace ON plan(workspace_id);

-- Workflow: DAG canvas container, one-to-one with a confirmed plan
CREATE TABLE workflow (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id UUID NOT NULL REFERENCES plan(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'running', 'paused', 'done')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_workflow_plan ON workflow(plan_id);

-- WorkflowNode: a single agent task slot on the canvas
-- task_id references agent_task_queue (the actual task table); nullable until enqueued
CREATE TABLE workflow_node (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id),
    title TEXT NOT NULL,
    prompt TEXT NOT NULL DEFAULT '',
    position_x FLOAT NOT NULL DEFAULT 0,
    position_y FLOAT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'queued', 'running', 'completed', 'failed', 'skipped')),
    task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_workflow_node_workflow ON workflow_node(workflow_id);
CREATE INDEX idx_workflow_node_task ON workflow_node(task_id);

-- WorkflowEdge: DAG ordering constraint (source -> target)
CREATE TABLE workflow_edge (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    source_node_id UUID NOT NULL REFERENCES workflow_node(id) ON DELETE CASCADE,
    target_node_id UUID NOT NULL REFERENCES workflow_node(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT workflow_edge_no_self CHECK (source_node_id != target_node_id),
    CONSTRAINT workflow_edge_unique UNIQUE (workflow_id, source_node_id, target_node_id)
);

CREATE INDEX idx_workflow_edge_workflow ON workflow_edge(workflow_id);
