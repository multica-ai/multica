-- TECH-3077: Skill observation tables for self-learning.
-- skill_observation: raw events emitted by the agent runtime when a skill
--   is used and the agent does something beyond the skill's instructions.
-- skill_observation_aggregate: per-skill/delta-type running totals used by
--   the pattern extractor to decide when to propose a change request.

CREATE TABLE IF NOT EXISTS skill_observation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id UUID NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    skill_version TEXT NOT NULL DEFAULT '',
    run_id UUID,
    autopilot_id UUID,
    delta_type TEXT NOT NULL,
    -- 'extra_table_access' | 'extra_verification_step' |
    -- 'output_format_deviation' | 'skill_chain_addition'
    delta_value TEXT NOT NULL,
    confidence FLOAT NOT NULL DEFAULT 1.0,
    outcome TEXT NOT NULL DEFAULT 'success',  -- 'success' | 'failure'
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_skill_obs_skill
    ON skill_observation(skill_id);
CREATE INDEX IF NOT EXISTS idx_skill_obs_type
    ON skill_observation(skill_id, delta_type);
CREATE INDEX IF NOT EXISTS idx_skill_obs_created
    ON skill_observation(created_at DESC);

CREATE TABLE IF NOT EXISTS skill_observation_aggregate (
    skill_id UUID NOT NULL REFERENCES skill(id) ON DELETE CASCADE,
    delta_type TEXT NOT NULL,
    delta_value TEXT NOT NULL,
    run_count INT NOT NULL DEFAULT 0,
    success_count INT NOT NULL DEFAULT 0,
    consistency_pct FLOAT NOT NULL DEFAULT 0.0,
    last_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    first_seen TIMESTAMPTZ NOT NULL DEFAULT now(),
    auto_proposed_at TIMESTAMPTZ,   -- NULL = proposal not yet generated
    PRIMARY KEY (skill_id, delta_type, delta_value)
);

CREATE INDEX IF NOT EXISTS idx_skill_obs_agg_skill
    ON skill_observation_aggregate(skill_id);
CREATE INDEX IF NOT EXISTS idx_skill_obs_agg_threshold
    ON skill_observation_aggregate(skill_id)
    WHERE run_count >= 10 AND consistency_pct >= 0.70 AND auto_proposed_at IS NULL;
