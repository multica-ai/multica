-- Revert workspace.repos to the v1 shape: {url, description}.
-- Local-only entries (no url) are dropped.

UPDATE workspace
SET repos = COALESCE((
    SELECT jsonb_agg(
        jsonb_build_object(
            'url', COALESCE(r->>'url', ''),
            'description', COALESCE(r->>'description', '')
        )
        ORDER BY ord
    )
    FROM jsonb_array_elements(repos) WITH ORDINALITY AS t(r, ord)
    WHERE (r->>'type' = 'github' OR r->>'type' IS NULL)
      AND COALESCE(r->>'url', '') <> ''
), '[]'::jsonb)
WHERE jsonb_typeof(repos) = 'array' AND jsonb_array_length(repos) > 0;
