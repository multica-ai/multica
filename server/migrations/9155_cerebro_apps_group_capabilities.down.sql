-- Remove mini-app lifecycle grants before restoring the previous capability
-- constraint.
DELETE FROM cerebro_group_capability
WHERE capability IN ('apps.create', 'apps.manage', 'apps.delete');

ALTER TABLE cerebro_group_capability
    DROP CONSTRAINT IF EXISTS cerebro_group_capability_known;

ALTER TABLE cerebro_group_capability
    ADD CONSTRAINT cerebro_group_capability_known CHECK (
        capability IN (
            'create_runtime',
            'create_agent',
            'create_shared_filters',
            'create_memory',
            'set_blocking_gate'
        )
    );
