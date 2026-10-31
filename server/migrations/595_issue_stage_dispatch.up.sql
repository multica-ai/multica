-- Server-side stage dispatch for one parent issue (#7345). While a row exists,
-- the unassigned sub-issues of the parent's first open stage are assigned to
-- squad_id as soon as every earlier stage is closed. dispatch_by is the member
-- who turned it on: the runs it starts answer to them. No foreign keys: the
-- code that deletes the issue, archives the squad or removes the member also
-- deletes the row.
CREATE TABLE IF NOT EXISTS issue_stage_dispatch (
    issue_id     UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    squad_id     UUID NOT NULL,
    dispatch_by  UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
