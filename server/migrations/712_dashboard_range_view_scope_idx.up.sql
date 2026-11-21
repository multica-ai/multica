-- Upsert key for RecordDashboardRangeView and the per-workspace lookup.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_dashboard_range_view_scope
    ON dashboard_range_view (workspace_id, day, requested_days, returned_days);
