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
-- UPGRADE SAFETY — the `originator_source IS NULL` clause exempts legacy-writer /
-- unbackfilled-lineage rows (Bohan chose the two-phase rollout on the MUL-4302 thread;
-- raised by Elon). `NOT VALID` skips the initial scan, but Postgres STILL checks a
-- pre-existing row whenever a later UPDATE touches it — even an UPDATE that leaves the
-- attribution columns alone. Cross-deployment stale queued/running tasks predate
-- `185_agent_task_accountable_user`, so they carry (originator_user_id set,
-- accountable_user_id NULL); their next claim / complete / cancel by the new backend
-- would fail a bare invariant CHECK.
--
-- `originator_source` was added by `184` with no default/backfill, so a NULL there does
-- NOT strictly mean "row predates the migration" — it means the row was written by a
-- writer that does not populate attribution: (a) rows created before this PR's
-- migrations; (b) during a rolling deploy, NEW rows an older, not-yet-replaced backend
-- INSERTs against the migrated schema; (c) a retry clone that inherits a legacy row's
-- NULL lineage. All three are exactly the rows the transition must let keep flowing,
-- and all resolve to "no attribution recorded", so exempting them is correct. Every
-- attribution-aware write goes through finalizeAttribution, which always stamps a
-- non-NULL source, so those rows stay under the FULL equality CHECK. Phase 3 backfills
-- the exempt rows (real source + reconciled accountable), then drops this constraint
-- and re-adds the strict form (without the exemption) + VALIDATE.
--
-- This does NOT reopen the #5192 bug class: that merge — and every real enqueue /
-- coalesce path — stamps originator_source non-NULL, so an (originator=B,
-- accountable=A) bypass is still rejected. Only the source-NULL (legacy-writer /
-- unbackfilled) shape is exempt.
--
-- Added NOT VALID so the constraint is a fast metadata-only add on the hot queue table
-- (no full-table ACCESS EXCLUSIVE scan); it enforces on every new INSERT/UPDATE
-- immediately.
--
-- The `accountable_user_id IS NOT NULL AND` guard is load-bearing: a bare
-- `accountable_user_id = originator_user_id` would let (originator=X, accountable=NULL)
-- slip through — SQL evaluates `NULL = X` to UNKNOWN, and a CHECK passes on anything
-- that is not FALSE. The bypass we most need to catch (originator set, accountable
-- left NULL) is exactly that shape, so the NULL must be rejected explicitly.
ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_accountable_matches_originator
    CHECK (
        originator_source IS NULL
        OR originator_user_id IS NULL
        OR (accountable_user_id IS NOT NULL AND accountable_user_id = originator_user_id)
    )
    NOT VALID;
