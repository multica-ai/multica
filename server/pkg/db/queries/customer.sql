-- name: ListCustomers :many
SELECT c.*,
       count(p.id)::bigint AS project_count
FROM customer c
LEFT JOIN project p ON p.customer_id = c.id
WHERE c.workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR c.status = sqlc.narg('status'))
GROUP BY c.id
ORDER BY lower(c.name) ASC, c.created_at DESC;

-- name: GetCustomerInWorkspace :one
SELECT c.*,
       count(p.id)::bigint AS project_count
FROM customer c
LEFT JOIN project p ON p.customer_id = c.id
WHERE c.id = $1 AND c.workspace_id = $2
GROUP BY c.id;

-- name: CreateCustomer :one
INSERT INTO customer (
    workspace_id, name, description, website, email, phone, status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: UpdateCustomer :one
UPDATE customer SET
    name = COALESCE(sqlc.narg('name'), name),
    description = sqlc.narg('description'),
    website = sqlc.narg('website'),
    email = sqlc.narg('email'),
    phone = sqlc.narg('phone'),
    status = COALESCE(sqlc.narg('status'), status),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteCustomer :exec
DELETE FROM customer WHERE id = $1;

-- name: CountProjectsByCustomer :one
SELECT count(*) FROM project
WHERE customer_id = $1;

-- name: ListProjectsByCustomer :many
SELECT * FROM project
WHERE workspace_id = $1 AND customer_id = $2
ORDER BY created_at DESC;
