-- Reverse of 060_repo_binding.up.sql: re-add `workspace.repos` JSONB column,
-- rehydrate it from the workspace-scoped bindings, then drop the new tables.
-- Project- and issue-scoped bindings (added by later migrations) cannot be
-- represented in the JSONB shape and are dropped along with the tables.

ALTER TABLE workspace ADD COLUMN repos JSONB NOT NULL DEFAULT '[]';

UPDATE workspace w
SET repos = COALESCE((
    SELECT jsonb_agg(
               jsonb_build_object('url', r.url, 'description', rb.description)
               ORDER BY r.url
           )
    FROM repo_binding rb
    JOIN repo r ON r.id = rb.repo_id
    WHERE rb.scope_type = 'workspace' AND rb.scope_id = w.id
), '[]'::jsonb);

DROP TABLE repo_binding;
DROP TABLE repo;
