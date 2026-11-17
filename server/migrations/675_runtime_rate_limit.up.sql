-- Latest subscription rate-limit snapshot per runtime (MUL-9427): how much of
-- the agent CLI account's 5-hour / weekly windows is used and when each
-- resets, as the CLI itself reported during the runtime's most recent task.
-- One row per runtime, replaced by newer observations.
--
-- No foreign keys (repo rule): runtime deletion (DeleteAgentRuntime,
-- DeleteAgentRuntimesByProfile) and workspace teardown remove these rows in
-- the same statements that delete the runtimes.
--
-- Upstream published this table as 665_runtime_rate_limit and, in the same
-- number, 665_weixin_state. Prefixes from 129 up must be unique. weixin_state
-- stays on the published name: its CREATE TABLE is not idempotent, so a
-- renamed copy would fail or rebuild login state on databases that already
-- recorded 665_weixin_state. This file is the idempotent newcomer, moved to
-- the next free prefix. Databases that already recorded
-- 665_runtime_rate_limit keep that ledger row (the key is the full
-- filename). If the table is already there, leave it — do not rebuild it
-- and do not drop the old ledger row.
DO $$
BEGIN
  IF to_regclass('runtime_rate_limit') IS NOT NULL THEN
    RAISE NOTICE 'migration report: runtime_rate_limit already exists; not rebuilding. Legacy ledger key 665_runtime_rate_limit is left in place when present';
    RETURN;
  END IF;

  CREATE TABLE runtime_rate_limit (
      runtime_id   UUID PRIMARY KEY,
      workspace_id UUID NOT NULL,
      snapshot     JSONB NOT NULL,
      observed_at  TIMESTAMPTZ NOT NULL,
      updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
  );
END $$;
