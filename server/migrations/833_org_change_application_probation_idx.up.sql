-- The probation job reads only the windows that are open and not yet decided.
CREATE INDEX CONCURRENTLY IF NOT EXISTS org_change_application_probation_idx
    ON org_change_application (probation_ends_at)
    WHERE probation_outcome IS NULL AND reverted_at IS NULL AND probation_ends_at IS NOT NULL;
