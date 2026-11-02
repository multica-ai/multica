-- The Secretary's accounted questions to squad leaders (spec 2.5, plan 9.8). A
-- row is written with the comment that asks the question, so the daily cap is
-- counted rather than requested. When the squad answers on the same issue, the
-- row is marked answered and the next digest carries the answer.
--
-- asked_on is the UTC day of the ask; the cap counts one workspace's rows for
-- one day. No foreign keys: the workspace, the Secretary, the squad, the issue
-- and the comment are validated in application code.
--
-- The primary key is built concurrently in 715 and attached in 716; the
-- (workspace_id, asked_on) index is 717.
CREATE TABLE IF NOT EXISTS brief_inquiry (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    asked_by_agent_id UUID NOT NULL,
    squad_id UUID NOT NULL,
    issue_id UUID,
    comment_id UUID,
    question TEXT NOT NULL CHECK (char_length(question) BETWEEN 1 AND 1000),
    asked_on DATE NOT NULL,
    answered_at TIMESTAMPTZ,
    answer_excerpt TEXT CHECK (char_length(answer_excerpt) <= 280),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
