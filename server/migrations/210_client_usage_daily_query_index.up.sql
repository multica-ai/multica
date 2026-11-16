CREATE INDEX CONCURRENTLY client_usage_daily_activity_client_user_idx ON client_usage_daily (activity_date, client_type, user_id);
