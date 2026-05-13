-- Cerebro workflow engine, phase 2 extension (JEH-1114, PR 1 of 3).
--
-- JEH-619 surfaced three orchestration gaps the phase-1 / phase-2 actions
-- don't cover. JEH-1114 closes them with three new primitives. PR 1 adds the
-- first one; PR 3 adds the second action (escalate_to_owner). validate_evidence
-- (PR 2) is a CONDITION op, not a new action_type, so it doesn't touch this
-- CHECK constraint.
--
--   route_by_domain — classify an issue (code / business / design / content)
--                     and attach a `<prefix><domain>` label that downstream
--                     workflows can react to. Composes cleanly with phase-1
--                     conditions instead of forcing a multi-step engine
--                     refactor (vej A in the JEH-1114 RFC).
--
-- The CHECK is rebuilt rather than relaxed because Postgres can't extend an
-- IN-list in place. DROP + ADD in the same transaction keeps the validation
-- gap to a single statement boundary.
--
-- Idempotent: re-running the migration on an already-migrated DB is a no-op
-- because `DROP CONSTRAINT IF EXISTS` is no-op when the rebuilt list already
-- contains route_by_domain (it just gets re-asserted).

ALTER TABLE cerebro_workflow
    DROP CONSTRAINT IF EXISTS cerebro_workflow_action_known;

ALTER TABLE cerebro_workflow
    ADD CONSTRAINT cerebro_workflow_action_known CHECK (
        action_type IN (
            'set_status',
            'create_sub_issue',
            'send_reminder',
            'run_skill',
            'comment_on_issue',
            'route_by_domain'
        )
    );
