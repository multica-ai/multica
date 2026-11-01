ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS prior_session_input_tokens;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS prior_session_turns;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS session_resume_decision;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS session_resume_reason;
