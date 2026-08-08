-- Inbox v2, step 1: give inbox_item the columns it needs to act as the event
-- table of a group/event model, without moving any data.
--
-- The product renders "one row per issue" but the database only ever had "one
-- row per event", so the thing users actually operate on has no entity. Rather
-- than build a parallel event table and migrate onto it, inbox_item KEEPS its
-- role and its original 15 columns; the group becomes a new sibling table and
-- these five nullable columns are the join between them.
--
-- All five are nullable and stay nullable. Every existing row has NULL here and
-- is claimed later, lazily, per user — there is no migration day and no
-- backfill that must finish before the deploy is safe. `read` and `archived`
-- keep working exactly as they do today; once groups exist they become mirrors
-- of group state, but they are NOT dropped: the mobile long tail reads them,
-- and event-level dismissal (an issue completing silently retires its stale
-- task_failed row) is a per-row behaviour a group-level flag cannot express.
ALTER TABLE inbox_item
    -- The group this event belongs to. NULL means "not yet claimed", which is
    -- what every pre-existing row is and what reconcile looks for.
    ADD COLUMN group_id     UUID,
    -- Per-group monotonic sequence, allocated under the group row lock. The
    -- read cursor compares against this rather than created_at: same-millisecond
    -- events, backfilled rows and multi-node clock skew all break timestamp
    -- ordering, and a mis-ordered cursor silently marks unread events read.
    ADD COLUMN event_seq    BIGINT,
    -- The jump target, promoted out of the `details` JSON drawer. Untyped
    -- details is why a status-change row could open an unrelated comment: when
    -- the newest row had no target the clients borrowed one from an older
    -- event. Typed columns make that unrepresentable.
    ADD COLUMN target_kind  TEXT,
    ADD COLUMN target_id    UUID,
    -- Producer-derived idempotency key, stable across retries and independent
    -- of the row id. Nullable because legacy rows predate it.
    ADD COLUMN delivery_key TEXT;
