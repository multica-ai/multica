-- Project Notes: free-form markdown notepads scoped to a project. Users create
-- as many as they want (a running journal, a conclusions page, scratch notes);
-- each one is plain markdown owned by Multica, NOT a pointer to an external
-- system. That is the line between this table and project_resource: resources
-- are typed pointers (github_repo / local_directory / …) whose ref only locates
-- something elsewhere, while a note's content lives here.
--
-- Notes are deliberately absent from the agent runtime brief. The brief has no
-- length ceiling and is injected verbatim on every task, so an unbounded number
-- of notes would compete with the task itself for context. Agents discover the
-- notepads through the multica-platform builtin skill and pull only what they
-- need via `multica project note list` / `note get`.
--
-- No foreign keys per repository convention: project/workspace deletion cleans
-- notes up explicitly in application code inside a transaction.
CREATE TABLE project_note (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL,
    workspace_id UUID NOT NULL,
    title        TEXT NOT NULL,
    -- Markdown body. Starts empty so "create a blank notepad" is one INSERT
    -- with no body, and grows via append.
    body_md      TEXT NOT NULL DEFAULT '',
    -- Manual ordering within a project, mirroring project_resource.position.
    position     INT  NOT NULL DEFAULT 0,
    -- Author. Nullable because an agent-created note has no member behind it.
    created_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
