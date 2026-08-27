-- GAP-24 (fork #17): opt-in verification pass after a work task completes.
-- verify_agent_id names the agent that runs the verification pass whenever
-- the owning agent completes an issue task that produced a branch. NULL =
-- feature disabled for the agent (default; no behavior change).
--
-- Deliberately NO foreign key (repo rule): referential integrity is enforced
-- in the application layer — the enqueue path re-loads and validates the
-- verifier at use time, so a deleted verifier degrades to "no verification"
-- instead of blocking completions.
ALTER TABLE agent ADD COLUMN verify_agent_id UUID;
