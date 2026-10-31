-- Deleting an issue is a hard delete: the row and its dependents go away, and
-- the workspace's number counter is never rewound, so the number gap is the
-- only trace left behind. Callers addressing an issue by its identifier
-- (GET /api/issues/{PREFIX-N}, the CLI `issue get`) could not tell that gap
-- apart from a number that was never issued (#9070) — both answered the same
-- not-found, so a lead inspecting a vanished issue had no way to see it ever
-- existed and would file a replacement instead.
--
-- This table records the (workspace_id, number) of every deleted issue at
-- delete time, inside the same transaction as DeleteIssue, so the detail
-- endpoint can answer a deleted identifier with a machine-readable tombstone
-- while a never-issued number keeps returning not found. It mirrors the
-- tombstone concept of comment.deleted_at (migration 471) without softening
-- the issue row itself: nothing here changes what any issue query returns —
-- deleted issues stay out of every list, exactly as before. Numbers are never
-- reused (the per-workspace counter only increments), so the pair is stable.
--
-- Deleting a workspace cascades away its tombstones: with the workspace gone
-- there is no prefix left to resolve an identifier against.
CREATE TABLE deleted_issue (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    number INT NOT NULL,
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, number)
);
