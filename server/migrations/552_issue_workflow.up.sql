-- Project workflows (MUL-7420).
--
-- A workflow is a named, workspace-level selection of statuses from the
-- issue_status catalog, in the order a project's board shows them, with an
-- optional handoff on each step: entering the step assigns the issue to the
-- step's handler and starts its run with the step's instructions.
--
-- Statuses stay shared: steps reference issue_status by key, so issue.status
-- keeps holding a catalog key and cross-project filters, sorting and saved
-- views keep working unchanged. Projects opt in through project.workflow_id; a
-- project without one keeps the whole catalog (the implicit Default workflow),
-- so existing workspaces see no behavior change.
--
-- steps is JSONB because the editor saves a workflow as one unit and nothing
-- queries individual steps across workflows. Status keys and handler ids are
-- validated in application code; no foreign keys by project rule.
--
-- id is attached as the primary key in 553 from the index built CONCURRENTLY
-- in 552 (repo convention, see 332-334).
CREATE TABLE IF NOT EXISTS issue_workflow (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 256),
    initial_status_key TEXT NOT NULL,
    steps JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(steps) = 'array'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
