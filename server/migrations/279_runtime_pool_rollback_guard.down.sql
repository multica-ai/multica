DO $runtime_pool_rollback_guard$
BEGIN
    IF EXISTS (SELECT 1 FROM agent WHERE runtime_binding_mode = 'pool')
        OR EXISTS (SELECT 1 FROM agent_task_queue WHERE runtime_binding_mode = 'pool')
        OR EXISTS (SELECT 1 FROM platform_extension_release WHERE runtime_binding_mode = 'pool') THEN
        RAISE EXCEPTION 'runtime pool rows exist; rollback refused'
            USING ERRCODE = '23514';
    END IF;
END
$runtime_pool_rollback_guard$;
