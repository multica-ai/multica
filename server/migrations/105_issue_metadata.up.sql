-- Per-issue custom metadata: a small JSONB KV map agents use to record
-- pipeline state (PR number, pipeline_status, waiting_on, …) that doesn't
-- fit cleanly into the structured issue fields.
--
-- Application-level validation (handler) enforces key regex, key count
-- cap, and primitive-only values. The DB CHECKs below are defense-in-depth
-- against migrations or direct SQL writes that bypass the API surface.
ALTER TABLE issue ADD COLUMN metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE issue ADD CONSTRAINT issue_metadata_is_object
    CHECK (jsonb_typeof(metadata) = 'object');

ALTER TABLE issue ADD CONSTRAINT issue_metadata_size_limit
    CHECK (pg_column_size(metadata) <= 8192);

-- GIN with jsonb_path_ops: smaller index, faster `@>` containment.
-- We only need containment queries (no `?` existence), so the path-ops
-- variant is the right trade.
CREATE INDEX idx_issue_metadata_gin ON issue USING GIN (metadata jsonb_path_ops);
