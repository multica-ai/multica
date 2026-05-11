-- name: CreateCRMAccount :one
INSERT INTO crm_account (
    workspace_id, name, normalized_name, website, country, region, industry, status, owner_id, source, notes
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
) RETURNING id, workspace_id, name, normalized_name, website, country, region, industry, status, owner_id, source, notes, created_at, updated_at;

-- name: ListCRMAccounts :many
SELECT
    a.id, a.workspace_id, a.name, a.normalized_name, a.website, a.country, a.region, a.industry,
    a.status, a.owner_id, a.source, a.notes, a.created_at, a.updated_at,
    COUNT(c.id)::bigint AS contact_count
FROM crm_account a
LEFT JOIN crm_contact c ON c.account_id = a.id AND c.workspace_id = a.workspace_id
WHERE a.workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR a.status = sqlc.narg('status')::text)
  AND (sqlc.narg('search')::text IS NULL OR a.normalized_name LIKE '%' || sqlc.narg('search')::text || '%')
GROUP BY a.id
ORDER BY a.updated_at DESC, a.created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountCRMAccounts :one
SELECT count(*) FROM crm_account
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status')::text)
  AND (sqlc.narg('search')::text IS NULL OR normalized_name LIKE '%' || sqlc.narg('search')::text || '%');

-- name: GetCRMAccountInWorkspace :one
SELECT
    a.id, a.workspace_id, a.name, a.normalized_name, a.website, a.country, a.region, a.industry,
    a.status, a.owner_id, a.source, a.notes, a.created_at, a.updated_at,
    COUNT(c.id)::bigint AS contact_count
FROM crm_account a
LEFT JOIN crm_contact c ON c.account_id = a.id AND c.workspace_id = a.workspace_id
WHERE a.id = $1 AND a.workspace_id = $2
GROUP BY a.id;

-- name: UpdateCRMAccount :one
UPDATE crm_account SET
    name = $3,
    normalized_name = $4,
    website = $5,
    country = $6,
    region = $7,
    industry = $8,
    status = $9,
    owner_id = $10,
    source = $11,
    notes = $12,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING id, workspace_id, name, normalized_name, website, country, region, industry, status, owner_id, source, notes, created_at, updated_at;

-- name: DeleteCRMAccount :exec
DELETE FROM crm_account WHERE id = $1 AND workspace_id = $2;

-- name: CreateCRMContact :one
INSERT INTO crm_contact (
    workspace_id, account_id, name, email, phone, whatsapp_id, role_title, language, timezone, notes
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING id, workspace_id, account_id, name, email, phone, whatsapp_id, role_title, language, timezone, notes, created_at, updated_at;

-- name: ListCRMContactsByAccount :many
SELECT id, workspace_id, account_id, name, email, phone, whatsapp_id, role_title, language, timezone, notes, created_at, updated_at
FROM crm_contact
WHERE workspace_id = $1 AND account_id = $2
ORDER BY created_at ASC;

-- name: GetCRMContactInWorkspace :one
SELECT id, workspace_id, account_id, name, email, phone, whatsapp_id, role_title, language, timezone, notes, created_at, updated_at
FROM crm_contact
WHERE id = $1 AND workspace_id = $2;

-- name: UpdateCRMContact :one
UPDATE crm_contact SET
    account_id = $3,
    name = $4,
    email = $5,
    phone = $6,
    whatsapp_id = $7,
    role_title = $8,
    language = $9,
    timezone = $10,
    notes = $11,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING id, workspace_id, account_id, name, email, phone, whatsapp_id, role_title, language, timezone, notes, created_at, updated_at;

-- name: DeleteCRMContact :exec
DELETE FROM crm_contact WHERE id = $1 AND workspace_id = $2;

-- name: GetCRMAccountProfile :one
SELECT id, workspace_id, account_id, summary, profile_json, updated_by, created_at, updated_at
FROM crm_account_profile
WHERE account_id = $1 AND workspace_id = $2;

-- name: UpsertCRMAccountProfile :one
INSERT INTO crm_account_profile (
    workspace_id, account_id, summary, profile_json, updated_by
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (account_id) DO UPDATE SET
    summary = EXCLUDED.summary,
    profile_json = EXCLUDED.profile_json,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING id, workspace_id, account_id, summary, profile_json, updated_by, created_at, updated_at;

-- name: CreateCRMCommunicationNote :one
INSERT INTO crm_communication_note (
    workspace_id, account_id, contact_id, channel, direction, occurred_at, subject, body, created_by
) VALUES (
    $1, $2, $3, $4, $5, COALESCE($6, now()), $7, $8, $9
) RETURNING id, workspace_id, account_id, contact_id, channel, direction, occurred_at, subject, body, created_by, created_at, updated_at;

-- name: ListCRMCommunicationNotesByAccount :many
SELECT id, workspace_id, account_id, contact_id, channel, direction, occurred_at, subject, body, created_by, created_at, updated_at
FROM crm_communication_note
WHERE workspace_id = $1 AND account_id = $2
ORDER BY occurred_at DESC, created_at DESC
LIMIT $3 OFFSET $4;

-- name: CreateCRMEntityLink :one
INSERT INTO crm_entity_link (
    workspace_id, crm_entity_type, crm_entity_id, target_type, target_id, relation_type, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING id, workspace_id, crm_entity_type, crm_entity_id, target_type, target_id, relation_type, created_by, created_at;

-- name: ListCRMEntityLinksForCRMEntity :many
SELECT id, workspace_id, crm_entity_type, crm_entity_id, target_type, target_id, relation_type, created_by, created_at
FROM crm_entity_link
WHERE workspace_id = $1 AND crm_entity_type = $2 AND crm_entity_id = $3
ORDER BY created_at DESC;

-- name: DeleteCRMEntityLink :exec
DELETE FROM crm_entity_link WHERE id = $1 AND workspace_id = $2;
