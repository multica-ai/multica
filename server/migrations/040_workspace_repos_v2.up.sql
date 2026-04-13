-- Rewrite existing workspace.repos JSONB entries to the v2 schema.
-- v1: {url, description}
-- v2: {id, name, type, url?, local_path?, description}
--
-- All existing entries become type='github' since v1 only supported GitHub URLs.
-- A UUID id is generated per entry. The name is derived from the last URL
-- segment (stripping .git) when no explicit name exists.

UPDATE workspace
SET repos = COALESCE((
    SELECT jsonb_agg(
        jsonb_build_object(
            'id', gen_random_uuid()::text,
            'name', COALESCE(
                NULLIF(r->>'name', ''),
                NULLIF(
                    regexp_replace(
                        regexp_replace(COALESCE(r->>'url', ''), '\.git$', ''),
                        '^.*[/:]', ''
                    ),
                    ''
                ),
                'repo'
            ),
            'type', 'github',
            'url', COALESCE(r->>'url', ''),
            'description', COALESCE(r->>'description', '')
        )
        ORDER BY ord
    )
    FROM jsonb_array_elements(repos) WITH ORDINALITY AS t(r, ord)
), '[]'::jsonb)
WHERE jsonb_typeof(repos) = 'array' AND jsonb_array_length(repos) > 0;
