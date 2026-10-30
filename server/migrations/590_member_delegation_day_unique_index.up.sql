CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS member_delegation_day_user_date_uidx ON member_delegation_day (user_id, activity_date);
