# Project workflow changes

A project has one current workflow. Saving a custom definition or returning to the workspace default applies to every existing issue, including completed/closed work. Historical transition and execution records retain their original workflow IDs and policy snapshots.

## Preview and apply

`PUT /api/projects/{id}/issue-workflow` retains its existing definition fields and adds:

- `dry_run`: computes a plan in a rolled-back transaction.
- `status_mapping`: source status UUID to destination **spec key**, not the temporary UUID returned by a preview.
- `confirm_migration` and `migration_fingerprint`: acknowledge the previewed source snapshot.

`plan.migration` contains the affected issue/view counts, source statuses, safe default mappings, required choices, blocked issue IDs, and fingerprint. New issues, status changes, workflow edits, or affected view changes invalidate confirmation. Missing mappings, stale confirmations, active agent work, or storage failures leave the project, issues, and saved views unchanged. The request has a 30-second bound; timeout rolls back rather than partially publishing a migration.

```sh
multica project workflow apply PROJECT --file ./workflow.yml --dry-run
multica project workflow apply PROJECT --file ./workflow.yml \
  --status-map SOURCE_STATUS_UUID=target_key --confirm-migration FINGERPRINT
multica project workflow use-default PROJECT --dry-run
multica project workflow use-default PROJECT \
  --status-map SOURCE_STATUS_UUID=todo --confirm-migration FINGERPRINT
multica issue update ISSUE --project PROJECT --workflow-status TARGET_STATUS_UUID
```

Renaming, recoloring, reordering, and editing entry policies preserve status identity and do not migrate issues. Removing an occupied status or changing its phase uses the project apply preview; the individual status endpoints reject those operations while occupied. The shared workspace catalog retains its existing in-use deletion guard.

Project moves preserve an equivalent legacy node where one exists. Otherwise callers must supply `workflow_status_id` from the destination workflow. Project moves and workflow migrations record transitions without entering the target action, cancelling work, or notifying a parent that work completed. Queued/running agent work blocks the operation; it must be finished or explicitly cancelled first.

## Saved views and compatibility

Native saved status filters gain `query.statusFilterMappings`, a map of project UUID to source-status UUID / current-status UUID pairs. Table queries carry it as `filters.status_mappings`; client filtering uses the same project-scoped substitution. Unrelated projects keep their original predicate. Project-scoped views replace their own native status IDs directly. Repeated migrations compose mappings. Legacy string status filters retain compatibility-projection semantics.

No database schema migration or startup backfill is added. Existing mixed bindings are repaired by previewing and confirming the project's intended workflow, including when it is already in default mode. Never repair by resetting every issue to the initial status.

Deploy the backend before the updated clients. Older workflow clients can still read definitions and save non-migrating changes; a write needing migration returns 409 with a plan instead of silently leaving mixed data. Updated clients refuse to proceed if a server does not return the migration-preview contract. Clients predating this PR do not understand native saved-view mapping predicates; legacy saved views are unchanged. Once native views have been migrated, keep clients that understand this contract for those views. Reverting to the old server also restores its unsafe pointer-only writes, so workflow editing should be paused during such a rollback.

## Validation

Handler regression tests cover preview rollback, stale confirmation, progress preservation, terminal issues, status removal, queued-task blocking, scoped saved filters, and administrative project moves. Shared client tests cover preview cache isolation, mapping selection, and project-specific filter matching. Use the named local development environment for database-backed tests; do not use a production database.
