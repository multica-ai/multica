-- name: ListMcpConnectors :many
-- Returns the connector directory visible to a workspace: every global
-- (workspace_id IS NULL) curated connector UNION the workspace's own custom
-- connectors. Ordered by popularity so the directory leads with the most-used
-- integrations, with name as a stable tiebreaker.
SELECT * FROM mcp_connector
WHERE workspace_id IS NULL OR workspace_id = $1
ORDER BY popularity DESC, name ASC;

-- name: GetMcpConnector :one
-- Loads a single connector visible to the workspace (global or the
-- workspace's own custom row). The OR guard keeps tenant isolation: a custom
-- connector belonging to another workspace is never returned.
SELECT * FROM mcp_connector
WHERE id = $1 AND (workspace_id IS NULL OR workspace_id = $2);

-- name: CreateMcpConnector :one
-- Inserts a workspace-custom connector. workspace_id is always set here
-- (global rows come from the embedded seed, never this path).
INSERT INTO mcp_connector (
    workspace_id, slug, name, icon, description, popularity,
    input_schema, mcp_template, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: UpdateMcpConnector :one
-- Updates a workspace-custom connector. The workspace_id IS NOT NULL guard
-- makes global seed rows un-editable through this path, and the workspace_id
-- match enforces tenant isolation.
UPDATE mcp_connector SET
    name = COALESCE(sqlc.narg('name'), name),
    icon = COALESCE(sqlc.narg('icon'), icon),
    description = COALESCE(sqlc.narg('description'), description),
    popularity = COALESCE(sqlc.narg('popularity'), popularity),
    input_schema = COALESCE(sqlc.narg('input_schema'), input_schema),
    mcp_template = COALESCE(sqlc.narg('mcp_template'), mcp_template),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND workspace_id IS NOT NULL
RETURNING *;

-- name: DeleteMcpConnector :one
-- Deletes a workspace-custom connector. Same guards as Update: global rows are
-- protected (workspace_id IS NOT NULL) and tenant-isolated (workspace_id = $2).
-- Returns the deleted id so the handler can confirm a row actually matched
-- (guards against the silent-zero-rows class of bug, #1661).
DELETE FROM mcp_connector
WHERE id = $1 AND workspace_id = $2 AND workspace_id IS NOT NULL
RETURNING id;

-- name: CountGlobalMcpConnectors :one
-- Used by the idempotent seed-on-first-list to decide whether the global
-- catalog still needs to be inserted.
SELECT count(*) FROM mcp_connector WHERE workspace_id IS NULL;

-- name: GetGlobalMcpConnectorBySlug :one
-- Idempotency probe for the seeder: skip inserting a global connector whose
-- slug already exists.
SELECT * FROM mcp_connector
WHERE workspace_id IS NULL AND slug = $1;

-- name: InsertGlobalMcpConnector :exec
-- Inserts a single global (workspace_id NULL) curated connector from the
-- embedded seed. created_by is NULL for seeded rows. ON CONFLICT DO NOTHING
-- keeps concurrent seeders from racing on the global-slug partial unique
-- index. :exec (not :one) so a no-op insert on conflict returns no error —
-- a :one variant would surface pgx.ErrNoRows and 500 the cold-start seed.
INSERT INTO mcp_connector (
    workspace_id, slug, name, icon, description, popularity,
    input_schema, mcp_template, created_by
) VALUES (NULL, $1, $2, $3, $4, $5, $6, $7, NULL)
ON CONFLICT DO NOTHING;
