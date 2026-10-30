-- Restore the pre-actor type allowlist.
--
-- Fails closed when any actor / multi_actor definition still exists, the same
-- stance as 337: rewriting or deleting those rows to make the old constraint
-- fit would destroy user data. A definition row IS the field — issues key their
-- values by definition id, so deleting it orphans every value it holds and
-- re-running the up migration brings nothing back. Archived definitions count
-- too; their values stay resolvable, so they are just as costly to drop.
--
-- Rolling back a database that already carries actor fields therefore starts
-- with an explicit, auditable data migration that converts those definitions
-- (and the values issues hold for them) to a type the old allowlist accepts.
-- Only then is this direction safe to run.
--
-- The guard aborts before any DDL runs; the restored constraint is a second
-- line of defence that would reject the same rows on its own.
DO $$
DECLARE
    actor_defs BIGINT;
BEGIN
    SELECT count(*) INTO actor_defs
    FROM issue_property
    WHERE type IN ('actor', 'multi_actor');

    IF actor_defs > 0 THEN
        RAISE EXCEPTION 'cannot roll back 341: % actor/multi_actor property definition(s) still exist', actor_defs
            USING HINT = 'Convert those definitions and the issue values keyed to them to a pre-actor type first; this migration will not delete user data.';
    END IF;
END
$$;

ALTER TABLE issue_property DROP CONSTRAINT IF EXISTS issue_property_type_check;
ALTER TABLE issue_property ADD CONSTRAINT issue_property_type_check
    CHECK (type IN ('text', 'number', 'select', 'multi_select', 'date', 'checkbox', 'url'));
