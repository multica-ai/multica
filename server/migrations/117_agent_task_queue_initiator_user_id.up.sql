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
ALTER TABLE agent_task_queue
    ADD COLUMN initiator_user_id UUID REFERENCES "user"(id) ON DELETE SET NULL;
