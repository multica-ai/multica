-- FIR-1659 Fase 4: register the `create_shared_filters` group capability.
--
-- cerebro_group_capability (migration 9018) has a CHECK constraint listing the
-- known capability identifiers. Adding a new toggle requires widening it. The
-- Go-side allowlist (server/internal/cerebro/grouppermissions/permissions.go)
-- is kept in sync with this list.

ALTER TABLE cerebro_group_capability
    DROP CONSTRAINT IF EXISTS cerebro_group_capability_known;

ALTER TABLE cerebro_group_capability
    ADD CONSTRAINT cerebro_group_capability_known CHECK (
        capability IN ('create_runtime', 'create_agent', 'create_shared_filters')
    );
