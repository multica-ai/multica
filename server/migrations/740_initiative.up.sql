-- Initiatives (spec 3.1): one venture moving through a pipeline. The backing
-- issue carries the current stage as its status and is moved through the
-- workflow on every transition, so the workflow's handoff assigns the owner.
--
-- seq numbers the workspace's initiatives (INI-<seq>); it is allocated under the
-- workspace advisory lock. gate_days are the days after launch at which the
-- operate stage files its review gates. shelved_at and operating_since let a
-- resume shift the stage clock and let the gate schedule find its start.
--
-- No foreign keys. The primary key is built concurrently in 741, attached in
-- 742; the sequence and status indexes are 743 and 744.
CREATE TABLE IF NOT EXISTS initiative (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    pipeline_id UUID NOT NULL,
    seq INTEGER NOT NULL CHECK (seq >= 1),
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    thesis TEXT NOT NULL DEFAULT '' CHECK (char_length(thesis) <= 4000),
    stage_key TEXT NOT NULL,
    owner_squad_id UUID,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'shelved', 'operating', 'closed')),
    issue_id UUID NOT NULL,
    build_project_id UUID,
    source_proposal_id UUID,
    sponsor_user_id UUID,
    gate_days INTEGER[] NOT NULL DEFAULT '{30,90}',
    stage_entered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    shelved_at TIMESTAMPTZ,
    operating_since TIMESTAMPTZ,
    closed_at TIMESTAMPTZ,
    closed_reason TEXT NOT NULL DEFAULT '' CHECK (char_length(closed_reason) <= 2000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
