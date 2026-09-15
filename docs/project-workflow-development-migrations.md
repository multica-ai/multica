# Updating a database used with the WIP project workflow branch

The unpublished project workflow migrations were renumbered from 472–488 to
491–507 to follow the migrations now on `main`. Their SQL is unchanged. Fresh
installations and databases that only ran `main` can use `make migrate-up` normally.

A development database that already ran the old workflow migrations must rename
its existing migration records before `make migrate-up`. Otherwise the runner
would replay the foundation migration against tables that already exist. Do not
roll back the workflow migrations: that would remove the workflow data.

Stop writes to that development environment, back up the database, and execute
this transaction against its managed database. It updates only matching applied
version names, preserves application rows, and fails on a conflicting new record
rather than silently claiming a migration was applied. Do not insert records for
migrations that have not run.

```sql
BEGIN;
UPDATE schema_migrations AS applied
SET version = renames.new_version
FROM (VALUES
    ('472_issue_workflow_foundation', '491_issue_workflow_foundation'),
    ('473_issue_workflow_pkey_index', '492_issue_workflow_pkey_index'),
    ('474_issue_workflow_status_pkey_index', '493_issue_workflow_status_pkey_index'),
    ('475_issue_transition_pkey_index', '494_issue_transition_pkey_index'),
    ('476_automation_execution_pkey_index', '495_automation_execution_pkey_index'),
    ('477_issue_workflow_primary_keys', '496_issue_workflow_primary_keys'),
    ('478_issue_workflow_scope_index', '497_issue_workflow_scope_index'),
    ('479_issue_workflow_legacy_status_index', '498_issue_workflow_legacy_status_index'),
    ('480_issue_transition_revision_index', '499_issue_transition_revision_index'),
    ('481_automation_execution_trigger_index', '500_automation_execution_trigger_index'),
    ('482_issue_transition_timeline_index', '501_issue_transition_timeline_index'),
    ('483_issue_workflow_binding_index', '502_issue_workflow_binding_index'),
    ('484_agent_task_automation_execution_index', '503_agent_task_automation_execution_index'),
    ('485_issue_workflow_backfill', '504_issue_workflow_backfill'),
    ('486_automation_execution_task_status', '505_automation_execution_task_status'),
    ('487_issue_workflow_spec_fields', '506_issue_workflow_spec_fields'),
    ('488_issue_workflow_spec_key_index', '507_issue_workflow_spec_key_index')
) AS renames(old_version, new_version)
WHERE applied.version = renames.old_version;
COMMIT;
```

Then run `make migrate-up` from the same checkout and resume the managed
environment. The runner applies the new `main` migrations and skips workflow
migrations already recorded under their new names. This is a one-time procedure
for databases used with the unpublished branch, not a runtime compatibility path.
