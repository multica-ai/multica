ALTER TABLE agent_task_queue
    ADD COLUMN skip_session_resume BOOLEAN NOT NULL DEFAULT false;

COMMENT ON COLUMN agent_task_queue.skip_session_resume IS
    'When true, server skips the usual "resume previous session" lookup on task claim. '
    'Used when the issue''s skill definition or instructions have changed and a fresh '
    'conversation is required.';
