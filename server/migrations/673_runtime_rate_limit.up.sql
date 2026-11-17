-- Latest subscription rate-limit snapshot per runtime (MUL-9427): how much of
-- the agent CLI account's 5-hour / weekly windows is used and when each
-- resets, as the CLI itself reported during the runtime's most recent task.
-- One row per runtime, replaced by newer observations.
--
-- No foreign keys (repo rule): runtime deletion (DeleteAgentRuntime,
-- DeleteAgentRuntimesByProfile) and workspace teardown remove these rows in
-- the same statements that delete the runtimes.
--
-- This fork previously recorded the same table as 675_runtime_rate_limit,
-- and an older cut used 665_runtime_rate_limit. Those filenames must not
-- return: 665 is weixin_state and 675 is chat_wakeup. CREATE TABLE IF NOT
-- EXISTS leaves an existing table in place, including one those ledger
-- rows already created.
CREATE TABLE IF NOT EXISTS runtime_rate_limit (
    runtime_id   UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    snapshot     JSONB NOT NULL,
    observed_at  TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
