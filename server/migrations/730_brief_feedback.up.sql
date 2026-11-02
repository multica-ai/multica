-- The boss's rating of a brief (spec 2.8, plan 9.12). One row per (brief,
-- member): a second submission replaces the first in place, so the row always
-- holds the member's current view of that brief.
--
-- rating is 'useful' or 'not_useful'. tags is a subset of the five reasons the
-- brief dialog offers; the CHECK keeps the set closed even if a caller skips
-- the handler's validation. note is optional and short. member_id is the
-- member's user id, as brief.created_by_member_id and brief_delivery.member_id
-- are.
--
-- No foreign keys: the workspace, brief and member are validated in application
-- code. The primary key is built concurrently in 731 and attached in 732; the
-- (brief_id, member_id) key that the upsert conflicts on is 733, and the
-- (workspace_id, updated_at) window index is 734.
CREATE TABLE IF NOT EXISTS brief_feedback (
    id           UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    brief_id     UUID NOT NULL,
    member_id    UUID NOT NULL,
    rating       TEXT NOT NULL CHECK (rating IN ('useful', 'not_useful')),
    tags         TEXT[] NOT NULL DEFAULT '{}'
        CHECK (tags <@ ARRAY['too_long', 'missing_something', 'wrong', 'too_vague', 'not_for_me']::text[]),
    note         TEXT NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
