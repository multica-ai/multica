CREATE TABLE issue_share (
    issue_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    token text NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
