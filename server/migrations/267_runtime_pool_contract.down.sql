DO $runtime_pool_rollback_preflight$
BEGIN
    IF EXISTS (SELECT 1 FROM agent WHERE runtime_binding_mode = 'pool')
        OR EXISTS (SELECT 1 FROM agent_task_queue WHERE runtime_binding_mode = 'pool')
        OR EXISTS (SELECT 1 FROM platform_extension_release WHERE runtime_binding_mode = 'pool') THEN
        RAISE EXCEPTION 'runtime pool rows exist; rollback refused'
            USING ERRCODE = '23514';
    END IF;
END
$runtime_pool_rollback_preflight$;

DROP TABLE agent_comment_followup_obligation;

ALTER TABLE platform_extension_release
    DROP CONSTRAINT platform_extension_release_runtime_routing_check;

ALTER TABLE platform_extension_release
    DROP CONSTRAINT platform_extension_release_runtime_binding_mode_check;

ALTER TABLE platform_extension_release
    ADD CONSTRAINT platform_extension_release_check CHECK (
        (runtime_id IS NULL AND squad_id IS NULL)
        OR (runtime_id IS NOT NULL AND squad_id IS NOT NULL)
    );

ALTER TABLE platform_extension_release
    DROP COLUMN runtime_requirements;

ALTER TABLE platform_extension_release
    DROP COLUMN runtime_binding_mode;

ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_routing_lifecycle_check;

ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_removed_check;

ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_unresolved_check;

ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_explicit_fresh_check;

ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_fixed_snapshot_check;

ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_affinity_pair_check;

ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_status_check;

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_status_check CHECK (
        status IN (
            'queued',
            'deferred',
            'dispatched',
            'running',
            'waiting_local_directory',
            'completed',
            'failed',
            'cancelled'
        )
    );

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_active_requires_runtime
    CHECK (runtime_id IS NOT NULL OR completed_at IS NOT NULL)
    NOT VALID;

ALTER TABLE agent_task_queue
    DROP COLUMN explicit_fresh_session;

ALTER TABLE agent_task_queue
    DROP COLUMN session_affinity_runtime_id;

ALTER TABLE agent_task_queue
    DROP COLUMN session_affinity_state;

ALTER TABLE agent_task_queue
    DROP COLUMN runtime_requester_user_id;

ALTER TABLE agent_task_queue
    DROP COLUMN placement_workspace_id;

ALTER TABLE agent_task_queue
    DROP COLUMN runtime_requirements;

ALTER TABLE agent_task_queue
    DROP COLUMN runtime_binding_mode;

ALTER TABLE agent_runtime
    DROP COLUMN capabilities;

ALTER TABLE agent
    DROP CONSTRAINT agent_runtime_binding_mode_check;

ALTER TABLE agent
    ADD CONSTRAINT agent_runtime_mode_check CHECK (
        runtime_mode IN ('local', 'cloud')
    );

ALTER TABLE agent
    DROP COLUMN runtime_requirements;

ALTER TABLE agent
    DROP COLUMN runtime_binding_mode;

ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_terminal_completed_at_check;
