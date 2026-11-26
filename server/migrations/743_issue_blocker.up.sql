CREATE TABLE issue_blocker (
 workspace_id uuid NOT NULL,
 issue_id uuid NOT NULL,
 blocker_issue_id uuid NOT NULL,
 created_by_type text NOT NULL,
 created_by_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK (issue_id <> blocker_issue_id)
);
