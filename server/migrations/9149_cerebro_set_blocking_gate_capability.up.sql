-- Register the `set_blocking_gate` group capability (default deny). FIR-3496.
--
-- cerebro_group_capability (migration 9018) has a CHECK constraint listing the
-- known capability identifiers. Adding the blocking-gate right requires widening
-- it. The Go-side allowlist
-- (server/internal/cerebro/grouppermissions/permissions.go) is kept in sync.
--
-- Default deny is enforced by absence: a group has the capability only when a
-- row exists, so no backfill is needed — every group starts without it. A
-- workspace admin always passes the check regardless of the grant.

ALTER TABLE cerebro_group_capability
    DROP CONSTRAINT IF EXISTS cerebro_group_capability_known;

ALTER TABLE cerebro_group_capability
    ADD CONSTRAINT cerebro_group_capability_known CHECK (
        capability IN ('create_runtime', 'create_agent', 'create_shared_filters', 'create_memory', 'set_blocking_gate')
    );
