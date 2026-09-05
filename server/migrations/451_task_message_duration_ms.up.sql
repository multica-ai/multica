-- Provider-reported tool-call duration (multica-ai/multica#8025). OpenCode
-- measures each tool call itself (part.state.time.start/end); without a column
-- to carry it across the transcript boundary, the run view rendered every
-- OpenCode call as 0s. NULL = the provider reported no duration (all other
-- runtimes today); >= 0 = the provider measured the call. created_at keeps its
-- server-clock semantics untouched.
ALTER TABLE task_message
    ADD COLUMN duration_ms BIGINT;
