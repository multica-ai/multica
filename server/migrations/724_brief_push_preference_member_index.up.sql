-- One preference per member. The upsert in the preference API conflicts on this
-- key, so the lookup and the write agree on who the row belongs to.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS brief_push_preference_member_uidx ON brief_push_preference (workspace_id, member_id);
