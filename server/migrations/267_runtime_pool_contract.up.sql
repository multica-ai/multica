DO $runtime_pool_terminal_timestamp_preflight$
DECLARE
    terminal_without_completed BIGINT;
    nonterminal_with_completed BIGINT;
BEGIN
    SELECT
        count(*) FILTER (
            WHERE status IN ('completed', 'failed', 'cancelled')
              AND completed_at IS NULL
        ),
        count(*) FILTER (
            WHERE status NOT IN ('completed', 'failed', 'cancelled')
              AND completed_at IS NOT NULL
        )
    INTO terminal_without_completed, nonterminal_with_completed
    FROM agent_task_queue;

    IF terminal_without_completed <> 0 OR nonterminal_with_completed <> 0 THEN
        RAISE EXCEPTION
            'runtime_pool_terminal_timestamp_preflight terminal_without_completed=% nonterminal_with_completed=%',
            terminal_without_completed,
            nonterminal_with_completed
            USING ERRCODE = '23514';
    END IF;
END
$runtime_pool_terminal_timestamp_preflight$;

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_terminal_completed_at_check
    CHECK ((status IN ('completed', 'failed', 'cancelled')) = (completed_at IS NOT NULL))
    NOT VALID;

ALTER TABLE agent_task_queue
    VALIDATE CONSTRAINT agent_task_queue_terminal_completed_at_check;

ALTER TABLE agent
    ADD COLUMN runtime_binding_mode TEXT NOT NULL DEFAULT 'fixed';

ALTER TABLE agent
    ADD COLUMN runtime_requirements JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE agent_runtime
    ADD COLUMN capabilities TEXT[] NOT NULL DEFAULT '{}'::text[];

ALTER TABLE agent_task_queue
    ADD COLUMN runtime_binding_mode TEXT NOT NULL DEFAULT 'fixed';

ALTER TABLE agent_task_queue
    ADD COLUMN runtime_requirements JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE agent_task_queue
    ADD COLUMN placement_workspace_id UUID;

ALTER TABLE agent_task_queue
    ADD COLUMN runtime_requester_user_id UUID;

ALTER TABLE agent_task_queue
    ADD COLUMN session_affinity_state TEXT NOT NULL DEFAULT 'none';

ALTER TABLE agent_task_queue
    ADD COLUMN session_affinity_runtime_id UUID;

ALTER TABLE agent_task_queue
    ADD COLUMN explicit_fresh_session BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE platform_extension_release
    ADD COLUMN runtime_binding_mode TEXT NOT NULL DEFAULT 'fixed';

ALTER TABLE platform_extension_release
    ADD COLUMN runtime_requirements JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE agent
    DROP CONSTRAINT agent_runtime_mode_check;

ALTER TABLE agent
    ADD CONSTRAINT agent_runtime_binding_mode_check CHECK (
        (runtime_binding_mode = 'fixed' AND runtime_mode IN ('local', 'cloud'))
        OR
        (runtime_binding_mode = 'pool' AND runtime_id IS NULL AND runtime_mode = 'pool')
    );

ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_status_check;

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_status_check CHECK (
        status IN (
            'waiting_runtime',
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
    ADD CONSTRAINT agent_task_queue_affinity_pair_check CHECK (
        (session_affinity_state = 'unresolved' AND session_affinity_runtime_id IS NULL)
        OR (session_affinity_state = 'none' AND session_affinity_runtime_id IS NULL)
        OR (session_affinity_state = 'pinned' AND session_affinity_runtime_id IS NOT NULL)
        OR (session_affinity_state = 'removed' AND session_affinity_runtime_id IS NULL)
    );

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_fixed_snapshot_check CHECK (
        runtime_binding_mode <> 'fixed'
        OR (
            session_affinity_state = 'none'
            AND session_affinity_runtime_id IS NULL
            AND placement_workspace_id IS NULL
            AND runtime_requester_user_id IS NULL
        )
    );

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_explicit_fresh_check CHECK (
        NOT explicit_fresh_session
        OR (runtime_binding_mode = 'pool' AND session_affinity_state = 'none')
    );

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_unresolved_check CHECK (
        session_affinity_state <> 'unresolved'
        OR (
            runtime_binding_mode = 'pool'
            AND chat_session_id IS NOT NULL
            AND status IN ('waiting_runtime', 'deferred')
            AND runtime_id IS NULL
            AND wait_reason IS NOT NULL
            AND wait_reason = 'chat_predecessor_pending'
        )
    );

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_removed_check CHECK (
        session_affinity_state <> 'removed'
        OR (
            runtime_binding_mode = 'pool'
            AND status = 'cancelled'
            AND completed_at IS NOT NULL
            AND runtime_id IS NULL
            AND wait_reason IS NOT NULL
            AND wait_reason = 'session_runtime_removed'
        )
    );

ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_active_requires_runtime;

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_routing_lifecycle_check CHECK (
        (
            runtime_binding_mode = 'fixed'
            AND status <> 'waiting_runtime'
            AND (
                (
                    status IN ('queued', 'deferred', 'dispatched', 'running', 'waiting_local_directory')
                    AND runtime_id IS NOT NULL
                )
                OR status IN ('completed', 'failed', 'cancelled')
            )
        )
        OR
        (
            runtime_binding_mode = 'pool'
            AND placement_workspace_id IS NOT NULL
            AND runtime_requester_user_id IS NOT NULL
            AND (
                (status IN ('waiting_runtime', 'deferred') AND runtime_id IS NULL)
                OR (
                    status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
                    AND runtime_id IS NOT NULL
                )
                OR status IN ('completed', 'failed', 'cancelled')
            )
        )
    );

ALTER TABLE platform_extension_release
    DROP CONSTRAINT platform_extension_release_check;

ALTER TABLE platform_extension_release
    ADD CONSTRAINT platform_extension_release_runtime_binding_mode_check CHECK (
        runtime_binding_mode IN ('fixed', 'pool')
    );

ALTER TABLE platform_extension_release
    ADD CONSTRAINT platform_extension_release_runtime_routing_check CHECK (
        (squad_id IS NULL AND runtime_id IS NULL)
        OR (
            runtime_binding_mode = 'fixed'
            AND squad_id IS NOT NULL
            AND runtime_id IS NOT NULL
        )
        OR (
            runtime_binding_mode = 'pool'
            AND squad_id IS NOT NULL
            AND runtime_id IS NULL
        )
    );

CREATE TABLE agent_comment_followup_obligation (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    comment_id UUID NOT NULL,
    comment_updated_at TIMESTAMPTZ NOT NULL,
    head_sha TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
