UPDATE workspace
SET repos = (
    SELECT COALESCE(
        jsonb_agg(elem - 'status'),
        '[]'::jsonb
    )
    FROM jsonb_array_elements(repos) AS elem
)
WHERE jsonb_array_length(repos) > 0;
