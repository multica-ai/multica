-- Backfill workspace.repos JSONB array entries with status="approved" so that
-- existing repos remain usable after we start enforcing the new approval
-- workflow. Skip entries that already carry a status (defensive — none should
-- exist today, but a no-op merge is safer than a clobber).
UPDATE workspace
SET repos = (
    SELECT COALESCE(
        jsonb_agg(
            CASE
                WHEN elem ? 'status' THEN elem
                ELSE elem || '{"status":"approved"}'::jsonb
            END
        ),
        '[]'::jsonb
    )
    FROM jsonb_array_elements(repos) AS elem
)
WHERE jsonb_array_length(repos) > 0;
