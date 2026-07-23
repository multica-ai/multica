-- Rollback for 9149_cerebro_set_blocking_gate_capability.up.sql. Restores the
-- four-value CHECK. Any set_blocking_gate rows must be removed first or the
-- constraint re-add will fail; delete them defensively.
DELETE FROM cerebro_group_capability WHERE capability = 'set_blocking_gate';

ALTER TABLE cerebro_group_capability
    DROP CONSTRAINT IF EXISTS cerebro_group_capability_known;

ALTER TABLE cerebro_group_capability
    ADD CONSTRAINT cerebro_group_capability_known CHECK (
        capability IN ('create_runtime', 'create_agent', 'create_shared_filters', 'create_memory')
    );
