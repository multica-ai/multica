-- Curator draft tasks dispatched to local daemon runtimes.
-- When runtime_mode=local, the server enqueues a row here and the daemon
-- polls a dedicated claim endpoint, executes the draft via direct LLM API
-- call, and reports the result back.
CREATE TABLE curator_draft_task (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    draft_kind TEXT NOT NULL CHECK (draft_kind IN ('issue', 'candidate', 'governance_finding')),
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'completed', 'failed')),
    input_data JSONB NOT NULL,
    result JSONB,
    error TEXT,
    created_by UUID NOT NULL REFERENCES member(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX curator_draft_task_workspace_status_idx ON curator_draft_task(workspace_id, status);
CREATE INDEX curator_draft_task_runtime_status_idx ON curator_draft_task(runtime_id, status);
