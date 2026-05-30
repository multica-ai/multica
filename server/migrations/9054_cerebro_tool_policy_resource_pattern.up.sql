-- 9054_cerebro_tool_policy_resource_pattern (FIR-2505 slice 1).
--
-- Adds the per-resource dimension to cerebro_tool_policy so an Allow/Ask/Deny
-- choice can target a specific resource pattern (e.g. one repo URL) instead of
-- only the capability as a whole. Existing rows carry resource_pattern = ''
-- which keeps the capability-wide semantics they had before: the empty string
-- is "applies to every resource for this capability". A non-empty value scopes
-- the row to that exact pattern, matched verbatim by the resolver.
--
-- The unique constraint and the lookup index both grow the column so two rows
-- can disagree on the same (tool, layer, subject) when they target different
-- resources — that is the whole point of the dimension.
--
-- Backward compatibility: every read path that omits resource_pattern continues
-- to see the legacy empty-string rows, and every existing test continues to
-- exercise the workspace-wide case. The dimension only becomes user-visible
-- when slice 2 (per-repo row emission in Table) and slice 3 (frontend
-- rendering) land — this migration is data-shape only.

ALTER TABLE cerebro_tool_policy
    ADD COLUMN IF NOT EXISTS resource_pattern TEXT NOT NULL DEFAULT '';

-- The legacy unique constraint allowed exactly one row per (tool, layer,
-- subject). With the resource dimension we need one per (tool, layer, subject,
-- resource_pattern) — the same agent can deny push on repoA while allowing it
-- on repoB. Drop the old constraint before adding the wider one.
ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_unique;

ALTER TABLE cerebro_tool_policy
    ADD CONSTRAINT cerebro_tool_policy_unique
        UNIQUE (workspace_id, tool_key, layer, subject_id, resource_pattern);

-- The hot-path lookup index includes resource_pattern so per-resource reads
-- can use the index. Existing capability-wide queries (resource_pattern = '')
-- still index-match on the prefix.
DROP INDEX IF EXISTS idx_cerebro_tool_policy_lookup;
CREATE INDEX IF NOT EXISTS idx_cerebro_tool_policy_lookup
    ON cerebro_tool_policy (workspace_id, tool_key, resource_pattern);
