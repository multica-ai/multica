ALTER TABLE project ADD COLUMN issue_prefix TEXT;
ALTER TABLE issue ADD COLUMN identifier_prefix TEXT;

UPDATE issue i SET identifier_prefix = UPPER(w.issue_prefix)
FROM workspace w WHERE w.id = i.workspace_id;

ALTER TABLE issue ALTER COLUMN identifier_prefix SET NOT NULL;
ALTER TABLE issue ADD CONSTRAINT issue_identifier_prefix_format
    CHECK (identifier_prefix = UPPER(identifier_prefix) AND identifier_prefix ~ '^[A-Z][A-Z0-9]{0,9}$');
ALTER TABLE project ADD CONSTRAINT project_issue_prefix_format
    CHECK (issue_prefix IS NULL OR (issue_prefix = UPPER(issue_prefix) AND issue_prefix ~ '^[A-Z][A-Z0-9]{0,9}$'));

CREATE TABLE issue_identifier_series (
    workspace_id UUID NOT NULL,
    prefix TEXT NOT NULL,
    next_number INT NOT NULL DEFAULT 0,
    project_id UUID,
    issued_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (prefix = UPPER(prefix) AND prefix ~ '^[A-Z][A-Z0-9]{0,9}$'),
    CHECK (next_number >= 0),
    CHECK ((next_number = 0 AND issued_at IS NULL) OR (next_number > 0 AND issued_at IS NOT NULL))
);

INSERT INTO issue_identifier_series (workspace_id, prefix, next_number, issued_at)
SELECT w.id, UPPER(w.issue_prefix), GREATEST(w.issue_counter, COALESCE(MAX(i.number), 0)),
       CASE WHEN GREATEST(w.issue_counter, COALESCE(MAX(i.number), 0)) > 0 THEN now() END
FROM workspace w LEFT JOIN issue i ON i.workspace_id = w.id
GROUP BY w.id, w.issue_prefix, w.issue_counter;

ALTER TABLE issue DROP CONSTRAINT IF EXISTS uq_issue_workspace_number;
DROP INDEX IF EXISTS idx_issue_workspace_number;
