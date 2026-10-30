CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_sys_cron_exec_job_plan
    ON sys_cron_executions (job_name, scope_kind, scope_id, plan_time DESC);
