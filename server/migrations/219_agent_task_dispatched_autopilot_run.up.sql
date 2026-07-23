-- Provenance marker for autopilot create_issue dispatch (MUL-4809 §4.1). Stamped
-- atomically at task INSERT with the autopilot_run this task was dispatched for, so
-- a crash between task enqueue and run.task_id bind can be repaired PRECISELY by
-- looking up the task carrying this run's id -- no time-window guessing, and an
-- ordinary comment/chat task (never stamped) can never be misattributed as the
-- run's dispatched work. No FK per CLAUDE.md; the relationship is resolved in app
-- code. Its lookup index is created CONCURRENTLY in migration 210.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS dispatched_autopilot_run_id UUID;
