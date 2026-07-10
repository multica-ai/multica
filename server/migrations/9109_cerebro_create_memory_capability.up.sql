-- Register the `create_memory` group capability (default deny).
--
-- cerebro_group_capability (migration 9018) has a CHECK constraint listing the
-- known capability identifiers. Adding the memory toggle requires widening it.
-- The Go-side allowlist (server/internal/cerebro/grouppermissions/permissions.go)
-- is kept in sync with this list.
--
-- Default deny is enforced by absence: a group has the capability only when a
-- row exists, so no backfill is needed — every group starts without it.

ALTER TABLE cerebro_group_capability
    DROP CONSTRAINT IF EXISTS cerebro_group_capability_known;

ALTER TABLE cerebro_group_capability
    ADD CONSTRAINT cerebro_group_capability_known CHECK (
        capability IN ('create_runtime', 'create_agent', 'create_shared_filters', 'create_memory')
    );
