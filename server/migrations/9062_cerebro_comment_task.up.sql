-- CEREBRO-PATCH(cerebro-comment-task): FIR-39 — per-comment cost on issues + channels.
--
-- The upstream `comment` table has no `task_id` column, so we cannot say "this
-- agent comment was authored by task X" from the row alone. Adding the column
-- upstream would mean a CEREBRO-PATCH on the canonical CreateComment SQL +
-- generated Go for every reader of `comment.*`. Instead we keep the link in a
-- net-new cerebro-side table: one row per agent comment, pinning the comment
-- to the task that produced it. The cost endpoint joins this to task_usage to
-- surface "cost $X" under the agent's reply (FIR-39, mirroring FIR-31 on chat).
--
-- The 1:1 PK on comment_id (with ON DELETE CASCADE) keeps the link garbage-
-- collected automatically when the comment row is removed. issue_id + task_id
-- denormalise the parent issue and the task that authored the comment so the
-- cost query can stay a single JOIN (no walking comment -> issue at read time).
CREATE TABLE IF NOT EXISTS cerebro_comment_task (
    comment_id   UUID PRIMARY KEY REFERENCES comment(id) ON DELETE CASCADE,
    task_id      UUID         NOT NULL,
    issue_id     UUID         NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    workspace_id UUID         NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Cost query path: for an issue, find every (task_id, latest comment) so the
-- badge lands on the run's final comment (the rule the FIR-39 mockup spelled
-- out: one run can post N comments — only show the cost on the last one).
CREATE INDEX IF NOT EXISTS idx_cerebro_comment_task_issue_task
    ON cerebro_comment_task(issue_id, task_id, created_at DESC);
