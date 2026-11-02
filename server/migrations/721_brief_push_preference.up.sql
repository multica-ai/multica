-- Each member's choice to receive the brief in a chat app (spec 2.7, plan
-- 9.10). One row per (workspace, member); a member who never opened the
-- settings has no row, which reads as brief_push off.
--
-- member_id is the member's user id, as brief.created_by_member_id and
-- channel_user_binding.multica_user_id are. binding_id is the chosen
-- channel_user_binding row. quiet_hours_start and quiet_hours_end are hours
-- 0..23 in the member's timezone; the window is [start, end), wrapping midnight
-- when start is after end. Both are NULL or both are set.
--
-- No foreign keys: the workspace, member and binding are validated in
-- application code. The primary key is built concurrently in 722 and attached
-- in 723; the (workspace_id, member_id) key is 724.
CREATE TABLE IF NOT EXISTS brief_push_preference (
    id                UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id      UUID NOT NULL,
    member_id         UUID NOT NULL,
    brief_push        BOOLEAN NOT NULL DEFAULT false,
    binding_id        UUID,
    quiet_hours_start SMALLINT CHECK (quiet_hours_start BETWEEN 0 AND 23),
    quiet_hours_end   SMALLINT CHECK (quiet_hours_end BETWEEN 0 AND 23),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((quiet_hours_start IS NULL) = (quiet_hours_end IS NULL)),
    CHECK (quiet_hours_start IS NULL OR quiet_hours_start <> quiet_hours_end)
);
