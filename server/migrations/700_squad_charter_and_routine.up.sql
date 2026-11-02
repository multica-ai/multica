-- A squad's standing job description (charter) and the routines that carry it
-- out. A routine is an ordinary autopilot plus the link row below;
-- the autopilot owns the schedule, runs and run history.
--
-- No foreign keys: the workspace, the squad and the autopilot are validated in
-- application code, and an autopilot's delete unlinks its routine in the same
-- transaction. The primary keys are built concurrently in 701/702 and attached
-- in 703; the squad_id listing index is 704.
--
-- squad_routine is keyed by autopilot_id: a routine is one autopilot, so the
-- autopilot is its identity and the row needs no separate id.
CREATE TABLE IF NOT EXISTS squad_charter (
    squad_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    mission TEXT NOT NULL DEFAULT '' CHECK (char_length(mission) <= 2000),
    responsibilities JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(responsibilities) = 'array'),
    outputs JSONB NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(outputs) = 'array'),
    boundaries TEXT NOT NULL DEFAULT '' CHECK (char_length(boundaries) <= 2000),
    escalation TEXT NOT NULL DEFAULT '' CHECK (char_length(escalation) <= 2000),
    intake TEXT NOT NULL DEFAULT '' CHECK (char_length(intake) <= 2000),
    response_hours INTEGER NOT NULL DEFAULT 48 CHECK (response_hours BETWEEN 1 AND 720),
    -- Switches the squad's weekly operating review on or off; on by default.
    operating_review_enabled BOOLEAN NOT NULL DEFAULT true,
    revision BIGINT NOT NULL DEFAULT 1,
    updated_by_type TEXT NOT NULL DEFAULT 'member',
    updated_by_id UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS squad_routine (
    autopilot_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    squad_id UUID NOT NULL,
    purpose TEXT NOT NULL CHECK (char_length(purpose) BETWEEN 1 AND 200),
    expected_output TEXT NOT NULL DEFAULT 'none'
        CHECK (expected_output IN ('none', 'issue', 'proposal', 'kb_document', 'report')),
    required BOOLEAN NOT NULL DEFAULT true,
    grace_hours INTEGER NOT NULL DEFAULT 24 CHECK (grace_hours BETWEEN 1 AND 336),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
