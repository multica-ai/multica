-- Converge environments that already applied the original constraint-bearing
-- version of migration 117 (which added `initiator_user_id` with a foreign key
-- to "user"). Those environments skip the now-edited 117 by version, so the FK
-- still exists there; this drops it so every environment ends up with a plain
-- `initiator_user_id` column and no FK.
--
-- The FK to the hot "user" table is what made 117 time out on a busy deploy, and
-- the column only feeds a best-effort name/email lookup at claim time, so the
-- constraint is not needed. DROP CONSTRAINT only touches catalog metadata (a
-- brief lock on agent_task_queue, no table scan, no lock on "user"). IF EXISTS
-- makes this a no-op where 117 already ran in its FK-free form. See MUL-2645.
ALTER TABLE agent_task_queue
    DROP CONSTRAINT IF EXISTS agent_task_queue_initiator_user_id_fkey;
