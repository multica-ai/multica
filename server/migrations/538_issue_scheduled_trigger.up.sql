-- One-time scheduled run bound to an issue (#5927). A member picks a future
-- instant on an already-assigned issue; the scheduler fires exactly one run
-- at that instant, independent of the issue's current status (unlike the
-- backlog -> todo automation, which only ever promotes an issue that is
-- already sitting in backlog).
--
-- Deliberately narrower than the autopilot `once` trigger kind sketched in
-- #5607: this table is issue-scoped, not autopilot-scoped. The maintainer
-- flagged the two as "designed together" but #5607 has no maintainer design
-- to build from yet, so this ships the smaller shape and reuses the same
-- scheduler primitive (server/internal/scheduler) a combined design can
-- build on later.
--
-- No FK to issue/workspace per repo convention — existence is validated in
-- application code (server/internal/service/issue_schedule.go).
CREATE TABLE issue_scheduled_trigger (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    run_at TIMESTAMPTZ NOT NULL,
    -- pending: waiting to fire. fired: successfully enqueued a run.
    -- cancelled: the creator (or issue delete) cancelled it before it fired.
    -- missed: run_at arrived but the enqueue attempt failed (issue deleted,
    -- assignee removed, agent archived, etc.) — see missed_policy.
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'fired', 'cancelled', 'missed')),
    -- What happens when the fire attempt fails. Only 'notify' exists today —
    -- the resolved answer on #5927 is "tell the creator, don't silently
    -- retry". The column exists (rather than being hardcoded) so a future
    -- combined design with #5607 can add e.g. 'retry' without another
    -- migration; the CHECK keeps it from silently accepting a value this
    -- code does not implement yet.
    missed_policy TEXT NOT NULL DEFAULT 'notify'
        CHECK (missed_policy IN ('notify')),
    -- The USER id of the member who created the schedule (not a member-row
    -- id) — matches the `actorUserID` / `OriginatorUserID` convention used
    -- everywhere else a human is attributed (server/internal/service/task.go).
    -- It is who the run is attributed to when the schedule fires and who
    -- gets notified if firing fails.
    created_by_user_id UUID NOT NULL,
    fired_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
