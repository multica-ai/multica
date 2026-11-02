-- One delivery per brief and member, so a subscriber that runs twice for one
-- brief inserts nothing new. Leading brief_id, this also serves the lookup of a
-- brief's deliveries.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS brief_delivery_brief_member_uidx ON brief_delivery (brief_id, member_id);
