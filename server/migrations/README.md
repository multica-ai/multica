## Limited schedule migration rename

`615_autopilot_schedule_limit` replaces `600_autopilot_schedule_limit` from
earlier revisions of this PR. Migration versions are full basenames, not
numeric prefixes. The unrelated `600_wecom_install_session` remains unchanged.

Upgrades from main, where the old schedule migration was never applied, need
no intervention. Existing triggers start with an unlimited limit and zero
accepted executions; historical runs are retained, without recounting them.

A database that already recorded `600_autopilot_schedule_limit` is outside the
automatic upgrade path. The new migration stops with an explicit error before
changing the table. Renaming the file alone would otherwise attempt to add the
same columns again. No automatic migration-history rewrite or counter reset is
performed. Deployment application status must be checked by its operator; it
cannot be inferred from the PR or the migration number.

For such a deployment, stop writers and back up both the database and its
migration history before proceeding. Keep the original binary and migration
files available for rollback. On a restored copy, verify the original migration
was fully applied, including both columns, defaults and check constraints, and
preserve every trigger's limit and execution count. An operator must reconcile
the old and new version records only after confirming that the schemas are
equivalent, then validate the upgrade on that copy before applying the same
reviewed procedure to the deployment. This change does not prescribe or execute
SQL against an existing deployment's `schema_migrations`.

Do not drop/re-add the columns or run the down migration to bypass the guard:
that discards accepted-execution counts and could allow exhausted schedules to
run again. A normal down migration also removes limits and counters, so it is
not a data-preserving rollback for deployments already using finite schedules.
