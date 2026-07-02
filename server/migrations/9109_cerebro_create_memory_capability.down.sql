-- Rollback for 9109_cerebro_create_memory_capability.up.sql. Restores the
-- three-value CHECK. Any create_memory rows must be removed first or the
-- constraint re-add will fail; delete them defensively.
DELETE FROM cerebro_group_capability WHERE capability = 'create_memory';

ALTER TABLE cerebro_group_capability
    DROP CONSTRAINT IF EXISTS cerebro_group_capability_known;

ALTER TABLE cerebro_group_capability
    ADD CONSTRAINT cerebro_group_capability_known CHECK (
        capability IN ('create_runtime', 'create_agent', 'create_shared_filters')
    );
