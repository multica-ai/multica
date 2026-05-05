-- Memory artifact: always-inject flag.
--
-- Today the runtime injection only delivers anchored artifacts (those tied
-- to a specific issue, project, agent, or channel). Free-floating artifacts
-- (anchor_type IS NULL) — workspace-wide rules of thumb like "How we deploy"
-- or "Brand voice rules" — sit in the Knowledge Base unread by agents.
--
-- This column gives users an explicit "Inject this into every task" knob.
-- Three alternatives considered and rejected:
--
--   - inject every free-floating artifact: blows the token budget. 50 wiki
--     pages × 100KB each = 5MB of CLAUDE.md per claim.
--   - inject by kind (e.g. always inject free-floating runbooks): too
--     implicit; users would be surprised when one of their notes shows up
--     in agent context they didn't know about.
--   - inject by tag (e.g. magic 'always-inject' tag): not discoverable,
--     and tag system is meant for arbitrary metadata.
--
-- Explicit column wins: queryable, indexable, gives the UI a clear
-- "Always-on context" checkbox affordance. The runtime caps the per-
-- claim count (default 5) so a misuse can't flood the agent's context.

ALTER TABLE memory_artifact
    ADD COLUMN always_inject_at_runtime BOOLEAN NOT NULL DEFAULT false;

-- Partial index for the runtime injection hot path. Most artifacts will
-- be `false`; only flagged ones get visited.
CREATE INDEX idx_memory_always_inject_active
    ON memory_artifact (workspace_id, updated_at DESC)
    WHERE always_inject_at_runtime = true AND archived_at IS NULL;
