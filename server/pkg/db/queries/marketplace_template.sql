-- name: CreateMarketplaceTemplate :one
INSERT INTO marketplace_template (
    source_workspace_id, created_by, source_type, source_id, name,
    description, tags, visibility, image_url, snapshot_version, snapshot
) VALUES (
    @source_workspace_id, @created_by, @source_type, sqlc.narg('source_id'), @name,
    @description, @tags, @visibility, sqlc.narg('image_url'), @snapshot_version, @snapshot
)
RETURNING *;

-- name: GetMarketplaceTemplate :one
SELECT * FROM marketplace_template WHERE id = $1;

-- name: ListVisibleMarketplaceTemplates :many
SELECT
    mt.id,
    mt.source_workspace_id,
    mt.created_by,
    mt.source_type,
    mt.source_id,
    mt.name,
    mt.description,
    mt.tags,
    mt.visibility,
    mt.image_url,
    mt.snapshot_version,
    mt.applied_count,
    mt.featured_at,
    mt.created_at,
    mt.updated_at,
    COALESCE(u.name, 'Multica') AS creator_name,
    COALESCE(jsonb_array_length(mt.snapshot->'agents'), 0)::integer AS agent_count,
    COALESCE(jsonb_array_length(mt.snapshot->'skills'), 0)::integer AS skill_count,
    COALESCE((
        SELECT jsonb_agg(
            jsonb_build_object(
                'key', agent.value->>'key',
                'name', agent.value->>'name',
                'description', COALESCE(agent.value->>'description', ''),
                'role', COALESCE((
                    SELECT member.value->>'role'
                    FROM jsonb_array_elements(COALESCE(mt.snapshot->'squad'->'members', '[]'::jsonb)) AS member(value)
                    WHERE member.value->>'agent_key' = agent.value->>'key'
                    LIMIT 1
                ), ''),
                'is_leader', agent.value->>'key' = COALESCE(mt.snapshot->'squad'->>'leader_key', '')
            )
            ORDER BY agent.ordinality
        )
        FROM jsonb_array_elements(COALESCE(mt.snapshot->'agents', '[]'::jsonb))
            WITH ORDINALITY AS agent(value, ordinality)
        WHERE agent.ordinality <= 4
    ), '[]'::jsonb) AS preview_agents
FROM marketplace_template mt
LEFT JOIN "user" u ON u.id = mt.created_by
WHERE (
        mt.visibility = 'public'
        OR (mt.source_workspace_id = @workspace_id AND mt.visibility = 'workspace')
        OR (mt.source_workspace_id = @workspace_id AND mt.visibility = 'private' AND mt.created_by = @user_id)
    )
    AND (@source_type::text = '' OR mt.source_type = @source_type)
    AND (
        @scope::text = ''
        OR (@scope = 'public' AND mt.visibility = 'public')
        OR (@scope = 'workspace' AND mt.source_workspace_id = @workspace_id AND mt.visibility IN ('workspace', 'public'))
        OR (@scope = 'private' AND mt.source_workspace_id = @workspace_id AND mt.visibility = 'private' AND mt.created_by = @user_id)
    )
    AND (
        @query::text = ''
        OR lower(mt.name) LIKE '%' || lower(@query) || '%'
        OR lower(mt.description) LIKE '%' || lower(@query) || '%'
        OR EXISTS (SELECT 1 FROM unnest(mt.tags) AS tag WHERE lower(tag) LIKE '%' || lower(@query) || '%')
    )
ORDER BY
    mt.featured_at DESC NULLS LAST,
    CASE WHEN @sort::text = 'recent' THEN mt.updated_at END DESC,
    CASE WHEN @sort::text = 'popular' OR @sort::text = '' THEN mt.applied_count END DESC,
    mt.updated_at DESC
LIMIT @page_size OFFSET @page_offset;

-- name: CountVisibleMarketplaceTemplates :one
SELECT count(*)
FROM marketplace_template mt
WHERE (
        mt.visibility = 'public'
        OR (mt.source_workspace_id = @workspace_id AND mt.visibility = 'workspace')
        OR (mt.source_workspace_id = @workspace_id AND mt.visibility = 'private' AND mt.created_by = @user_id)
    )
    AND (@source_type::text = '' OR mt.source_type = @source_type)
    AND (
        @scope::text = ''
        OR (@scope = 'public' AND mt.visibility = 'public')
        OR (@scope = 'workspace' AND mt.source_workspace_id = @workspace_id AND mt.visibility IN ('workspace', 'public'))
        OR (@scope = 'private' AND mt.source_workspace_id = @workspace_id AND mt.visibility = 'private' AND mt.created_by = @user_id)
    )
    AND (
        @query::text = ''
        OR lower(mt.name) LIKE '%' || lower(@query) || '%'
        OR lower(mt.description) LIKE '%' || lower(@query) || '%'
        OR EXISTS (SELECT 1 FROM unnest(mt.tags) AS tag WHERE lower(tag) LIKE '%' || lower(@query) || '%')
    );

-- name: UpdateMarketplaceTemplateMetadata :one
UPDATE marketplace_template SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    tags = COALESCE(sqlc.narg('tags')::text[], tags),
    visibility = COALESCE(sqlc.narg('visibility'), visibility),
    image_url = COALESCE(sqlc.narg('image_url'), image_url),
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: RefreshMarketplaceTemplateSnapshot :one
UPDATE marketplace_template SET
    snapshot = @snapshot,
    snapshot_version = @snapshot_version,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: IncrementMarketplaceTemplateAppliedCount :one
UPDATE marketplace_template SET
    applied_count = applied_count + 1,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteMarketplaceTemplate :execrows
DELETE FROM marketplace_template WHERE id = $1;
