-- The Secretary's written report to the boss for a period. Written by
-- POST /api/briefs; one inbox item per workspace owner announces it.
--
-- The author is the Secretary agent (created_by_agent_id) or, when an admin
-- member publishes by hand, that member (created_by_member_id): exactly one is
-- set.
--
-- No foreign keys: the workspace, the author and the proposals a brief raises
-- are validated in application code. The primary key is built concurrently in
-- 632/633 and the listing index in 634, following proposal (610-613).
CREATE TABLE IF NOT EXISTS brief (
    id                   UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL,
    kind                 TEXT NOT NULL CHECK (kind IN ('daily', 'weekly', 'ad_hoc')),
    period_start         TIMESTAMPTZ NOT NULL,
    period_end           TIMESTAMPTZ NOT NULL,
    title                TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    body                 TEXT NOT NULL CHECK (char_length(body) <= 20000),
    proposal_ids         UUID[] NOT NULL DEFAULT '{}',
    created_by_agent_id  UUID,
    created_by_member_id UUID,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((created_by_agent_id IS NULL) <> (created_by_member_id IS NULL))
);
