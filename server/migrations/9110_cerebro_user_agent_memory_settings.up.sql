-- Per (user × agent) memory toggle — the third memory access gate (locked spec
-- 29/6). Each user turns memory on for ONE agent, only for themselves. Two
-- independent switches: may read memory, may write memory. Both default OFF.
--
-- Default off is encoded as absence-of-row: a missing row means neither read nor
-- write, so no backfill is needed. A row is written only when a user enables at
-- least one switch on an agent. This gate is per-user and per-agent; it does NOT
-- apply to company memory (which is readable by all agents once the workspace
-- feature flag is on).
CREATE TABLE IF NOT EXISTS cerebro_user_agent_memory_settings (
    user_id          uuid NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    agent_id         uuid NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    can_read_memory  boolean NOT NULL DEFAULT false,
    can_write_memory boolean NOT NULL DEFAULT false,
    updated_at       timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_user_agent_memory_settings_agent
    ON cerebro_user_agent_memory_settings (agent_id);
