-- One rating per brief and member. The submit endpoint upserts on this key, so
-- a second submission replaces the first. Leading brief_id, it also serves the
-- lookup of a member's rating for one brief.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS brief_feedback_brief_member_uidx ON brief_feedback (brief_id, member_id);
