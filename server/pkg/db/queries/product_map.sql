-- name: ListProductNodesByWorkspace :many
SELECT * FROM product_nodes
WHERE workspace_id = $1
ORDER BY sort_order ASC, created_at ASC;

-- name: GetProductNode :one
SELECT * FROM product_nodes
WHERE id = $1 AND workspace_id = $2;

-- name: ListProductRefsByProduct :many
SELECT * FROM product_refs
WHERE product_id = $1
ORDER BY created_at ASC;

-- name: ListProductEditorsByProduct :many
SELECT * FROM product_editors
WHERE product_id = $1
ORDER BY created_at ASC;

-- name: IsProductEditor :one
SELECT EXISTS (
    SELECT 1 FROM product_editors
    WHERE product_id = $1 AND user_id = $2
) AS is_editor;

-- name: ListProductEditorsByWorkspace :many
SELECT pe.* FROM product_editors pe
JOIN product_nodes pn ON pn.id = pe.product_id
WHERE pn.workspace_id = $1
ORDER BY pe.created_at ASC;

-- name: UpsertProductNode :one
INSERT INTO product_nodes (workspace_id, parent_id, name, slug, description, sort_order, status, status_source, evidence)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (workspace_id, slug) DO UPDATE SET
    name = EXCLUDED.name,
    parent_id = EXCLUDED.parent_id,
    description = EXCLUDED.description,
    sort_order = EXCLUDED.sort_order,
    status = EXCLUDED.status,
    status_source = EXCLUDED.status_source,
    evidence = EXCLUDED.evidence,
    updated_at = now()
RETURNING *;

-- name: UpsertProductRef :one
INSERT INTO product_refs (product_id, ref_type, ref_id)
VALUES ($1, $2, $3)
ON CONFLICT (product_id, ref_type, ref_id) DO UPDATE SET created_at = product_refs.created_at
RETURNING *;

-- name: UpsertProductEditor :one
INSERT INTO product_editors (product_id, user_id)
VALUES ($1, $2)
ON CONFLICT (product_id, user_id) DO UPDATE SET created_at = product_editors.created_at
RETURNING *;
