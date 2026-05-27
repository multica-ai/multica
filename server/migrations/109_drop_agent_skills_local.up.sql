-- Clean up the unreleased per-agent local skill toggle. Fresh/prod databases
-- that never applied 108_agent_skills_local are unchanged, while dev/staging
-- databases that tested it converge back to the reverted schema.
ALTER TABLE agent DROP COLUMN IF EXISTS skills_local;
