-- Register the mini-app lifecycle group capabilities. FIR-3315.
--
-- The handlers already enforce these identifiers. This migration makes the
-- corresponding least-privilege grants authorable instead of forcing app
-- builders to hold the broader workspace admin role.

ALTER TABLE cerebro_group_capability
    DROP CONSTRAINT IF EXISTS cerebro_group_capability_known;

ALTER TABLE cerebro_group_capability
    ADD CONSTRAINT cerebro_group_capability_known CHECK (
        capability IN (
            'create_runtime',
            'create_agent',
            'apps.create',
            'apps.manage',
            'apps.delete',
            'create_shared_filters',
            'create_memory',
            'set_blocking_gate'
        )
    );
