-- One durable completion/review record per execution (v1.5 V5-2 EvidenceRecord
-- plus V5-7 review lifecycle fields incl. required review_version CAS). PK on
-- execution_id arrives via index + constraint.
CREATE TABLE execution_evidence_record (
    execution_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    schema_version INTEGER NOT NULL DEFAULT 1,
    runtime_evidence_state TEXT NOT NULL DEFAULT 'collecting' CHECK (runtime_evidence_state IN ('collecting', 'complete', 'failed')),
    output_ref JSONB,
    message_refs JSONB NOT NULL DEFAULT '[]'::jsonb,
    usage_refs JSONB NOT NULL DEFAULT '[]'::jsonb,
    artifact_refs JSONB NOT NULL DEFAULT '[]'::jsonb,
    test_refs JSONB NOT NULL DEFAULT '[]'::jsonb,
    review_policy TEXT NOT NULL DEFAULT 'none' CHECK (review_policy IN ('none', 'independent')),
    review_state TEXT NOT NULL DEFAULT 'not_required' CHECK (review_state IN ('not_required', 'pending', 'dispatching', 'queued', 'running', 'recorded', 'retry_wait', 'blocked')),
    review_version INTEGER NOT NULL DEFAULT 1,
    reviewer_agent_id UUID,
    review_task_id UUID,
    review_output_ref JSONB,
    review_attempt INTEGER NOT NULL DEFAULT 0,
    max_review_attempts INTEGER NOT NULL DEFAULT 3,
    review_next_wakeup TIMESTAMPTZ,
    review_lease_owner TEXT,
    review_lease_expires_at TIMESTAMPTZ,
    review_failure_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
