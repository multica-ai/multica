-- A retrospective trigger fired for a subject (spec 4.7). One row per firing.
-- The job fires each trigger at most once per subject per 14-day window, and
-- the row is the record of the window: the retrospective issue it opened is
-- issue_id. The subject is named by type and id, with no foreign keys: the
-- initiative, agent, squad, floor or company is validated when the job reads it,
-- and a later archive leaves a row that still names what fired.
CREATE TABLE IF NOT EXISTS retro_trigger (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    trigger_kind TEXT NOT NULL CHECK (trigger_kind IN (
        'initiative_closed', 'gate_rejected', 'agent_failures',
        'squad_rejections', 'budget_hold', 'stage_sla')),
    subject_type TEXT NOT NULL CHECK (subject_type IN (
        'initiative', 'agent', 'squad', 'floor', 'company')),
    subject_id UUID,
    issue_id UUID,
    evidence JSONB NOT NULL DEFAULT '{}',
    fired_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
