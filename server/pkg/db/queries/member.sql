-- name: ListMembers :many
SELECT * FROM multica_member
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetMember :one
SELECT * FROM multica_member
WHERE id = $1;

-- name: GetMemberByUserAndWorkspace :one
SELECT * FROM multica_member
WHERE user_id = $1 AND workspace_id = $2;

-- name: CreateMember :one
INSERT INTO multica_member (workspace_id, user_id, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: UpdateMemberRole :one
UPDATE multica_member SET role = $2
WHERE id = $1
RETURNING *;

-- name: DeleteMember :exec
DELETE FROM multica_member WHERE id = $1;

-- name: ListMembersWithUser :many
SELECT m.id, m.workspace_id, m.user_id, m.role, m.created_at,
       m.source, m.status, m.external_user_id, m.external_universal_id,
       m.employee_id, m.org_display_name, m.dept_id, m.dept_name,
       m.dept_path, m.position, m.is_main_department, m.dept_user_status,
       m.last_synced_at,
       u.name as user_name, u.email as user_email, u.avatar_url as user_avatar_url
FROM multica_member m
LEFT JOIN multica_user u ON u.id = m.user_id
WHERE m.workspace_id = $1
ORDER BY m.created_at ASC;

-- name: ListDeptMemberSnapshots :many
SELECT id, user_id, source, status, external_user_id, external_universal_id,
       employee_id, org_display_name, dept_id, dept_name, dept_path,
       position, is_main_department, dept_user_status, last_synced_at
FROM multica_member
WHERE workspace_id = $1;

-- name: UpsertDeptMember :one
INSERT INTO multica_member (
    workspace_id, user_id, role, source, status,
    external_user_id, external_universal_id, employee_id, org_display_name,
    dept_id, dept_name, dept_path, position, is_main_department,
    dept_user_status, last_synced_at
)
VALUES (
    $1, $2, 'member', 'dept', $3,
    $4, $5, $6, $7,
    $8, $9, $10, $11, $12,
    $13, $14
)
ON CONFLICT (workspace_id, external_universal_id)
WHERE external_universal_id IS NOT NULL AND external_universal_id <> ''
DO UPDATE SET
    user_id = COALESCE(EXCLUDED.user_id, multica_member.user_id),
    status = EXCLUDED.status,
    external_user_id = EXCLUDED.external_user_id,
    employee_id = EXCLUDED.employee_id,
    org_display_name = EXCLUDED.org_display_name,
    dept_id = EXCLUDED.dept_id,
    dept_name = EXCLUDED.dept_name,
    dept_path = EXCLUDED.dept_path,
    position = EXCLUDED.position,
    is_main_department = EXCLUDED.is_main_department,
    dept_user_status = EXCLUDED.dept_user_status,
    last_synced_at = EXCLUDED.last_synced_at
RETURNING *;
