-- Human Attribution — enforce the one-way invariant at the DB layer (MUL-4302;
-- decided by Bohan + Elon on the MUL-4302 thread). The application collapses
-- originator == accountable at a single chokepoint (finalizeAttribution) and every
-- write path takes both from the same attribution.Result — but the #5192 comment-
-- coalescing merge proved that ANOTHER feature's code can silently bypass that
-- chokepoint and leave originator=B / accountable=A. A cross-column CHECK is exactly
-- the class of bug this guards: any future write that breaks the invariant fails at
-- enqueue time rather than silently mis-attributing an audited run.
--
--     originator_user_id IS NOT NULL  ⟹  accountable_user_id = originator_user_id
--
-- This is NOT the source-enum CHECK the earlier ruling forbade (that ban existed so
-- a new source label needs no migration); this locks a two-column equality, not an
-- enumerable value, and carries no FK / cascade.
--
-- Added NOT VALID: it enforces on every new INSERT/UPDATE immediately, but does NOT
-- scan or reject pre-migration rows — historical runs may carry originator with a
-- NULL accountable (the column did not exist yet). Those are the Phase 3 backfill
-- target; VALIDATE CONSTRAINT runs once backfill has reconciled them.
--
-- The `accountable_user_id IS NOT NULL AND` guard is load-bearing: a bare
-- `accountable_user_id = originator_user_id` would let (originator=X, accountable=NULL)
-- slip through — SQL evaluates `NULL = X` to UNKNOWN, and a CHECK passes on anything
-- that is not FALSE. The bypass we most need to catch (originator set, accountable
-- left NULL) is exactly that shape, so the NULL must be rejected explicitly.
ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_accountable_matches_originator
    CHECK (
        originator_user_id IS NULL
        OR (accountable_user_id IS NOT NULL AND accountable_user_id = originator_user_id)
    )
    NOT VALID;
