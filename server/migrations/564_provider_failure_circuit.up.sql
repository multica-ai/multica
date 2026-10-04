CREATE TABLE provider_failure_circuit (
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    thread_root_id UUID NOT NULL,
    provider_error_signature TEXT NOT NULL,
    failing_agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    first_failure_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_failure_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    failure_count INTEGER NOT NULL DEFAULT 1,
    causal_depth INTEGER NOT NULL DEFAULT 1,
    last_failure_task_id UUID NOT NULL,
    recovery_started BOOLEAN NOT NULL DEFAULT false,
    recovery_revision INTEGER NOT NULL DEFAULT 0,
    open_until TIMESTAMPTZ,
    action_required_comment_id UUID REFERENCES comment(id) ON DELETE SET NULL DEFERRABLE INITIALLY DEFERRED,
    PRIMARY KEY (issue_id, thread_root_id, provider_error_signature, failing_agent_id)
);

CREATE INDEX idx_provider_failure_circuit_open
    ON provider_failure_circuit (open_until)
    WHERE open_until IS NOT NULL;

-- Historical duplicate comments may predate platform idempotency. Keep the
-- earliest task as the consumer receipt and clear only the duplicate evidence
-- stamp before installing the forward guard; the task rows/results remain.
CREATE TABLE agent_task_origin_event_dedupe_backup (
    task_id UUID PRIMARY KEY REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    trigger_evidence_kind TEXT NOT NULL,
    trigger_evidence_ref_id UUID NOT NULL
);

WITH duplicates AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY trigger_evidence_ref_id, agent_id
               ORDER BY created_at, id
           ) AS position
    FROM agent_task_queue
    WHERE trigger_evidence_kind = 'comment'
      AND trigger_evidence_ref_id IS NOT NULL
      AND parent_task_id IS NULL
      AND rerun_of_task_id IS NULL
), backed_up AS (
    INSERT INTO agent_task_origin_event_dedupe_backup (
        task_id, trigger_evidence_kind, trigger_evidence_ref_id
    )
    SELECT task.id, task.trigger_evidence_kind, task.trigger_evidence_ref_id
    FROM agent_task_queue task
    JOIN duplicates ON duplicates.id = task.id
    WHERE duplicates.position > 1
    RETURNING task_id
)
UPDATE agent_task_queue AS task
SET trigger_evidence_kind = NULL,
    trigger_evidence_ref_id = NULL
FROM backed_up
WHERE task.id = backed_up.task_id;

CREATE UNIQUE INDEX idx_agent_task_origin_event_consumer
    ON agent_task_queue (trigger_evidence_ref_id, agent_id)
    WHERE trigger_evidence_kind = 'comment'
      AND trigger_evidence_ref_id IS NOT NULL
      AND parent_task_id IS NULL
      AND rerun_of_task_id IS NULL;
