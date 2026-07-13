-- Capture the real task initiator (the requester behind this run) on chat
-- tasks at enqueue time. chat_session.creator_id is NOT a reliable initiator:
-- Lark group sessions deliberately set the creator to the installer (a stable
-- workspace identity that survives group-member churn), not the person who sent
-- the triggering message. Storing the actual sender here lets the daemon brief
-- attribute the run to the right person instead of the installer/owner.
--
-- NULL for non-chat tasks and for chat tasks queued before this column existed;
-- the brief simply omits the `## Task Initiator` section in that case. See
-- MUL-2645.
--
-- Plain UUID, no FK to "user": adding a foreign key here also takes a lock on
-- the (hot) "user" table at migration time, which made this ALTER time out on a
-- busy production deploy. The column only feeds a best-effort name/email lookup
-- at claim time (a stale id just yields no initiator section), so referential
-- integrity is not load-bearing. Migration 118 drops the FK on environments
-- that already applied the original constraint-bearing version of this file.
ALTER TABLE agent_task_queue
    ADD COLUMN initiator_user_id UUID;
