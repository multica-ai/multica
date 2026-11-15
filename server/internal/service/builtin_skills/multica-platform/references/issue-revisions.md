# Conditional issue writes

Read the issue with `multica issue get <id> --output json` and retain its
server-owned `revision`. Pass that value as `--expected-revision` to protect a
write against concurrent changes:

```bash
multica issue update <id> --title "Reconciled title" --expected-revision 7 --output json
multica issue status <id> in_progress --expected-revision 7 --output json
multica issue assign <id> --to-id <agent-id> --expected-revision 7 --output json
multica issue reorder <id> --top --expected-revision 7 --output json
multica issue metadata set <id> --key state --value ready --expected-revision 7 --output json
multica issue metadata delete <id> --key state --expected-revision 7 --output json
multica issue property set <id> --name Severity --value High --expected-revision 7 --output json
multica issue property unset <id> --name Severity --expected-revision 7 --output json
multica issue label add <id> <label-id> --expected-revision 7 --output json
multica issue label remove <id> <label-id> --expected-revision 7 --output json
```

These are independent examples, not a sequence using the same revision. A
state-changing conditional request advances the revision once. A matched no-op
preserves its revision; a stale no-op still conflicts. Read the resulting
revision before the next write. Main issue commands return `revision`;
metadata/property/label mutations return `issue_revision` alongside their values.

A stale precondition changes no protected field, association, or revision.
With `--output json`, the command emits a structured `revision_conflict` to
stdout with `expected_revision` and `actual_revision`, then exits nonzero.
Re-read the issue and reconcile the intended change before retrying. Never
blindly replace the expected revision with the latest number.

For an atomic replacement of a label set, or a combined field/property/label
change, use `issue batch update` with exactly one issue and its revision:

```bash
multica issue batch update <id> --clear-labels --add-label <label-id> --expected-revision 7 --output json
```

The scalar precondition rejects multiple issue arguments before any requests.
The result row includes the final `issue_revision`. Combined batch operations
can advance it more than once inside their single transaction; use the returned
value rather than calculating the next revision. Select `--output json` to
receive revisions; table output keeps its existing presentation.

Omitting the flag keeps the existing request and output formats, including
unconditional no-op behavior. All conditional writes require a server with
conditional mutation routes. Older servers reject those routes;
the CLI never retries an unconditional write instead.

`issue update` rejects `--expected-revision` combined with `--attachment` or
`--pr-auto-complete` before sending any requests. File uploads and the PR
completion override are separate mutations, so those combinations cannot make
one atomic issue write. Reading description text from a local file remains
supported. Comment edits use their own comment revision; conditional issue
comment appends are not provided here.
