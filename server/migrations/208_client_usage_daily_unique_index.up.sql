CREATE UNIQUE INDEX CONCURRENTLY client_usage_daily_identity_date_uidx ON client_usage_daily (user_id, client_type, install_id, activity_date);
