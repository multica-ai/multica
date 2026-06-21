-- Rollback for 9094_cerebro_shared_filters_capability.up.sql. Restores the
-- original two-value CHECK. Any create_shared_filters rows must be removed first
-- or the constraint re-add will fail; delete them defensively.
DELETE FROM cerebro_group_capability WHERE capability = 'create_shared_filters';

ALTER TABLE cerebro_group_capability
    DROP CONSTRAINT IF EXISTS cerebro_group_capability_known;

ALTER TABLE cerebro_group_capability
    ADD CONSTRAINT cerebro_group_capability_known CHECK (
        capability IN ('create_runtime', 'create_agent')
    );
