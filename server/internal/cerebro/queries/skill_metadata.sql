-- Skill metadata queries — category/domain/tag filtering and metadata sync.
-- Part of TECH-3077 skill library unlock.

-- name: ListSkillsByMetadata :many
SELECT id, workspace_id, name, description, config, metadata,
       created_by, created_at, updated_at, owner_id, approver_ids, current_version
FROM skill
WHERE workspace_id = @workspace_id
  AND (COALESCE(@category::text, '') = '' OR metadata->>'category' = @category)
  AND (COALESCE(@domain::text, '') = '' OR metadata->>'domain' = @domain)
  AND (COALESCE(@tag::text, '') = '' OR metadata->'tags' ? @tag)
  AND (COALESCE(@status::text, '') = '' OR metadata->>'status' = @status)
  AND (COALESCE(@data_domain::text, '') = '' OR metadata->'data_domains' @> @data_domain::jsonb)
ORDER BY name ASC;

-- name: UpdateSkillMetadata :exec
UPDATE skill SET metadata = @metadata, updated_at = now()
WHERE id = @id;

-- name: ListSkillsWithMissingMetadata :many
SELECT id, workspace_id, name, description, metadata, current_version
FROM skill
WHERE workspace_id = @workspace_id
  AND (
    metadata->>'category' IS NULL OR metadata->>'category' = '' OR
    metadata->>'domain' IS NULL OR metadata->>'domain' = '' OR
    metadata->>'status' IS NULL OR metadata->>'status' = ''
  )
ORDER BY name ASC;

-- name: GetSkillMetadata :one
SELECT metadata FROM skill
WHERE id = @id;
