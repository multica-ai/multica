-- A proposal is applied at most once: the apply path inserts this row under the
-- proposal's decision lock, and the key refuses a second application.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS org_change_application_proposal_uidx
    ON org_change_application (workspace_id, proposal_id);
