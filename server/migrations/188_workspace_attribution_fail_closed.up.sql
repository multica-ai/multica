-- Human Attribution, Phase 1 — per-workspace fail-closed policy (MUL-4302 §1/§3.5).
--
-- The accountable-human waterfall degrades to owner_fallback (accountable = agent
-- owner) when no precise human resolves, so every run has an accountable human by
-- default. Compliance/enterprise workspaces can instead opt into FAIL-CLOSED: a run
-- that cannot be attributed to a precise human is NOT enqueued rather than falling
-- back to the owner — better to block an unattributable run than to run it under a
-- degraded label.
--
-- Default FALSE preserves today's behavior (owner_fallback) for every existing
-- workspace. Adding a NOT NULL boolean WITH a constant default is a fast
-- metadata-only change on modern Postgres (no table rewrite).
ALTER TABLE workspace
    ADD COLUMN attribution_fail_closed BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN workspace.attribution_fail_closed IS
    'When TRUE, an agent run that resolves to no precise accountable human (would be owner_fallback) is refused at enqueue instead of degrading to the agent owner (MUL-4302 §3.5). Default FALSE = owner_fallback. Never affects authorization (originator_user_id).';
