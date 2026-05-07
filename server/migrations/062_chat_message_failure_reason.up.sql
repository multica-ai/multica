-- Mirror the issue path's "fallback comment on failure" with a failure_reason
-- column on chat_message. When FailTask runs on a chat task, server writes
-- an assistant chat_message tagged with the daemon-reported reason so the
-- conversation history shows what happened (instead of the previous black
-- hole where a failed task left no trace in the user-visible thread).
ALTER TABLE chat_message ADD COLUMN failure_reason TEXT;
