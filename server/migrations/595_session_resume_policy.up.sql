-- Session-resume accounting (STOE-29). A follow-up wakeup resumes the previous
-- (agent, issue) provider session, and an unbounded resume makes the next run
-- pay for the whole prior conversation. These columns record, on the run that
-- made the decision, how big the session it was offered was and what was done
-- about it, so a slow run is explainable from the run row alone.
--
-- prior_session_input_tokens is the largest input-side figure (input + cache
-- read + cache write) any single run in that session reported: the context a
-- resumed turn has to carry again. prior_session_turns counts the runs that
-- share the session. Both describe the session the claim was considering, so
-- they are recorded even when the answer was "do not resume" — that is the row
-- that explains a slow run. They stay NULL only when no session was on offer.
--
-- decision/reason are free TEXT rather than a CHECK so a new reason needs no
-- migration, matching the trigger_evidence_kind precedent. The vocabulary the
-- server writes today lives in internal/service/session_resume_policy.go:
--   decision: resumed | fresh | none
--   reason:   context_limit | turn_limit | forced_fresh | runtime_mismatch |
--             no_prior_session | metrics_unavailable
-- NULL on both means the run predates this migration, not that it decided
-- nothing.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS prior_session_input_tokens BIGINT;
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS prior_session_turns INTEGER;
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS session_resume_decision TEXT;
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS session_resume_reason TEXT;
