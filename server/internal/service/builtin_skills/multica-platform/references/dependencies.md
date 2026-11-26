# Dependencies

How one issue waits for others with `--blocked-by`, and what the wait does and
does not change.

## Waiting on other issues

`issue create` and `issue update` take repeatable `--blocked-by <issue>` flags
(UUID or identifier) that make an issue wait for other issues in the workspace.
A non-backlog issue enqueues its agent assignee once every blocker enters a done
or closed category (including cancellation), or is removed; a `backlog` issue
stays parked. `issue update --remove-blocked-by <issue>` removes a manual
dependency. `issue get` lists `blocked_by` references, each with
`via: manual|stage`, and `issue list --blocked` finds waiting work. An edge that
would form a cycle is rejected.

Blockers never change issue status; use the manual `blocked` status for missing
information or external conditions. Explicit mentions and replies still run.

To order sub-issues of one parent, use stages instead: earlier stages already
appear as `via: stage` blockers. See `references/sub-issues.md`.
