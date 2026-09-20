-- last_event_at is the created_at of the most recent task_message persisted for
-- this task: "when did this run last emit anything", denormalised onto the row
-- that answers "is this run still alive".
--
-- The task row already carries dispatched_at, started_at and completed_at, and
-- those three answer only when the run CHANGED STATE. A run that started five
-- hours ago and one that started five hours ago and wedged in its first minute
-- are the same row under that vocabulary, so any caller wanting "still moving?"
-- has to guess from status plus wall-clock age. The signal it wants is already
-- stored — task_message.created_at — just one table away from the row it is
-- looking at.
--
-- Denormalised rather than joined because the read is a list: the agent-tasks
-- endpoint returns every task for an agent, and answering by join means
-- max(created_at) per task_id over task_message, which grows per message rather
-- than per task, on a page that today reads one row per task and nothing else.
-- The column trades a few bytes per task row for keeping that read as cheap as
-- it is now. comment.last_activity_at computes the sibling question the other
-- way, by MAX over children; that read is per-issue, not a list of every task.
--
-- Nullable, and NULL is the normal state twice over: for every row written
-- before this migration, and for any task that has not yet emitted a message.
--
-- No index: the column is never a lookup key or a sort key. It is projected off
-- rows some other query already located by agent_id. That also keeps the bump a
-- non-indexed update, so the row rewrite can be HOT and leave this table's ~29
-- indexes untouched — measured heap-only on every bump in a local run.
--
-- No server logic branches on it. It is written by the two task_message writers
-- and projected read-only onto the agent-tasks response, so an external
-- orchestrator can compute now() - last_event_at itself. The daemon watchdogs
-- and the running-task sweeper are not wired to it.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS last_event_at TIMESTAMPTZ;

COMMENT ON COLUMN agent_task_queue.last_event_at IS
    'created_at of the newest task_message persisted for this task. NULL means no message has been reported yet, never "silent since the epoch" — a task dispatched a second ago has no message and is not late; measure a NULL row against created_at. Advanced best-effort by the task_message writers: the bump never blocks the message insert, so under concurrent writers to this row a bump can be skipped and the value trails by one batch. Not reset on completion, so it keeps the last message time after the run ends. Not comparable to completed_at: the cancel path stamps completed_at before the daemon flushes its final transcript, so last_event_at can be the later of the two.';
