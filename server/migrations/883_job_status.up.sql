-- The job status registry (spec 4.9): one row per scheduled job, written by the
-- scheduler when a plan finishes, so the registry reads a job's last success,
-- last failure and lag from here. Jobs are global, so there is no workspace
-- column. The scheduler's execution log is still the history and the source of
-- the stuck-run count.
CREATE TABLE IF NOT EXISTS job_status (
    job_name TEXT NOT NULL,
    last_run_at TIMESTAMPTZ NOT NULL,
    last_success_at TIMESTAMPTZ,
    last_failed_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
