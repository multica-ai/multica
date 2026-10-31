-- An agent result on an issue that a member has not seen yet (MUL-8438). Backs
-- the sidebar's unread dot. A row is written for each subscribed member when an
-- agent run on the issue ends, and removed when the member opens the issue.
-- Rows exist only while unread, so the table stays small and starts empty: no
-- issue lights up for runs that ended before this shipped.
--
-- outcome: completed | failed (from the run), or manual (marked unread by the
-- member). agent_id and task_id name the run, and are null for manual.
--
-- No foreign keys; issue and workspace deletion remove these rows in the
-- application deletion graph. Readers join current members. The primary key is
-- built concurrently in 598/599, following member_delegation_day (589-591).
CREATE TABLE IF NOT EXISTS issue_unread (
    workspace_id UUID NOT NULL,
    user_id      UUID NOT NULL,
    issue_id     UUID NOT NULL,
    outcome      TEXT NOT NULL CHECK (outcome IN ('completed', 'failed', 'manual')),
    agent_id     UUID,
    task_id      UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
