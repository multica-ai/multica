-- Give every workspace one explicit, stable Default Space.
--
-- Personal membership order is navigation state and must not decide where a
-- context-free create lands. Backfill the earliest active Space to preserve
-- the pre-migration fallback, then keep the choice explicit from here on.
ALTER TABLE workspace_space
    ADD COLUMN is_default BOOLEAN NOT NULL DEFAULT false;

WITH defaults AS (
    SELECT DISTINCT ON (workspace_id) id
    FROM workspace_space
    WHERE archived_at IS NULL
    ORDER BY workspace_id, created_at ASC, id ASC
)
UPDATE workspace_space s
SET is_default = true
FROM defaults d
WHERE s.id = d.id;

ALTER TABLE workspace_space
    ADD CONSTRAINT workspace_space_default_must_be_active
    CHECK (NOT is_default OR archived_at IS NULL);

CREATE UNIQUE INDEX uq_workspace_space_one_default
    ON workspace_space(workspace_id)
    WHERE is_default;
