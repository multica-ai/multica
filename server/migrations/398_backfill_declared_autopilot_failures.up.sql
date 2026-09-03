-- A run-only provider turn can complete normally while its final output says
-- the automation contract failed. Before the explicit failure marker existed,
-- two fail-fast prefixes were already documented in durable autopilot prompts.
-- Correct only rows whose first non-empty output line uses one of those exact
-- prefixes; later quoted/discussed failures remain successful.
UPDATE autopilot_run
SET status = 'failed',
    failure_reason = 'task declared failure: '
        || split_part(ltrim(result ->> 'output', E' \n\r\t'), E'\n', 1)
WHERE status = 'completed'
  AND jsonb_typeof(result) = 'object'
  AND (
      split_part(ltrim(result ->> 'output', E' \n\r\t'), E'\n', 1) LIKE 'Run failed fast:%'
      OR split_part(ltrim(result ->> 'output', E' \n\r\t'), E'\n', 1) LIKE 'Phase 4 verify FAILED:%'
  );
