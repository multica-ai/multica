-- The task row becomes cancelled when the server accepts a stop request, but
-- the local agent process can take a few seconds to observe that transition,
-- exit, and flush its transcript. Persist the daemon acknowledgement so UI
-- flows can distinguish "stop requested" from "process stopped".
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS cancel_acknowledged_at TIMESTAMPTZ;

COMMENT ON COLUMN agent_task_queue.cancel_acknowledged_at IS
    'Daemon-confirmed time the cancelled task process exited and its transcript was flushed.';
