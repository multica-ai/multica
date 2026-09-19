-- ListLabels counts assignments once per issue label, and label deletion also
-- looks up rows by label_id. The primary key starts with issue_id, so it cannot
-- selectively serve either path as the junction table grows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS issue_to_label_label_idx
    ON issue_to_label (label_id);
