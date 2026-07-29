-- Sensitive-operation approval flow (WS-721).
--
-- Two independent tables, application-owned lifecycle (no foreign keys, no
-- cascades - repo rule). Indexes live in follow-up single-statement migrations
-- 204-207 because CREATE INDEX CONCURRENTLY cannot share a migration file or
-- run inside a transaction.
--
-- v1 is single-step approval by a workspace owner/admin. current_step reserves
-- a multi-level extension point without altering the v1 path (always 1). The
-- polymorphic initiator/decider (member|agent) mirrors the rest of the actor
-- model so both humans and agents can request sensitive actions. The set of
-- operations that require approval is workspace-configurable via
-- workspace.settings JSONB, so no schema change is needed for the config bag.
--
-- approval_request  - one sensitive-action request and its terminal state.
-- approval_event     - append-only audit trail (the approval history).

CREATE TABLE approval_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    -- operation is the sensitive-action key, e.g. "workspace.delete",
    -- "issue.batch_delete", "member.role_downgrade_owner", "agent.delete",
    -- "project.delete". The built-in set lives in the handler; additional
    -- keys are workspace-configurable.
    operation TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id UUID,
    reason TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled', 'expired')),
    initiated_by_type TEXT NOT NULL CHECK (initiated_by_type IN ('member', 'agent')),
    initiated_by_id UUID NOT NULL,
    -- Reserved for multi-level approval; v1 is always 1.
    current_step INT NOT NULL DEFAULT 1,
    -- Set when status leaves pending (approved/rejected). Expired/cancelled
    -- keep these null and record the actor on the matching approval_event.
    decided_by_type TEXT CHECK (decided_by_type IN ('member', 'agent')),
    decided_by_id UUID,
    decided_at TIMESTAMPTZ,
    decision_comment TEXT NOT NULL DEFAULT '',
    -- operation-specific params, e.g. {"issue_ids": [...]} for batch delete. The
    -- handler always passes a JSON object (default "{}"); the column default is
    -- a backstop for direct SQL only.
    payload JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload) = 'object'),
    expires_at TIMESTAMPTZ,
    -- Track execution of the approved action (strong-blocking model: the
    -- action runs only after approval, recorded here for audit).
    executed_at TIMESTAMPTZ,
    execution_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE approval_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    approval_request_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    event_type TEXT NOT NULL CHECK (event_type IN (
        'created', 'approved', 'rejected', 'cancelled', 'expired',
        'executed', 'execution_failed'
    )),
    actor_type TEXT CHECK (actor_type IN ('member', 'agent', 'system')),
    actor_id UUID,
    comment TEXT NOT NULL DEFAULT '',
    details JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(details) = 'object'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
