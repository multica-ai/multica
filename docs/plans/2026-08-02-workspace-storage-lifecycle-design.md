# Workspace Storage Lifecycle Design

## Problem

Daemon-managed task roots are bounded by the existing lifecycle GC: terminal
issues are removed after `MULTICA_GC_TTL`, missing metadata is treated as an
orphan after `MULTICA_GC_ORPHAN_TTL`, and known build artifacts are removed
from completed but open tasks after `MULTICA_GC_ARTIFACT_TTL`. Deleting every
task root as soon as a process exits would break issue continuation, provider
session resume, and user-authored output kept for review.

The current disk incident has two narrower causes:

1. Each isolated Codex home recreates `codex-home/.tmp`, even though its
   observed contents are the same plugin and marketplace caches used by the
   shared Codex home. On the affected host this directory is about 305 MiB per
   Codex task.
2. Workspace roots have no Spotlight exclusion marker, so every new task and
   cache refresh is indexed.

The 4.7 GiB `c37d6ffd` outlier is not a runtime cache. It is a five-minute
LaunchAgent for WS-2512 that has produced 237 roughly 20 MiB observation runs
without retention. The runtime must not silently delete that live,
user-authored output while the parent issue is blocked.

## Considered approaches

### Delete the task root on process exit

This gives the smallest footprint, but violates the existing continuation and
resume contracts. It can also remove outputs that an issue still needs for
review. Rejected.

### Add an unconditional size cap for open issues

This bounds storage without waiting for issue closure, but an LRU decision
cannot distinguish disposable cache from durable work or an externally
scheduled process rooted in the task directory. Rejected until the product has
an explicit archive/retention contract for user output.

### Reclaim known caches and retain lifecycle GC

Keep the proven whole-root lifecycle, reclaim the known Codex runtime cache as
soon as its process exits, teach artifact GC to reclaim legacy per-task copies,
and exclude the workspace root from Spotlight. Selected because it removes the
structural retained-per-run multiplier without deleting state whose durability
is part of the task contract or exposing executable plugin caches across tasks.

## Design

### Ephemeral Codex runtime cache

Codex marketplace checkouts contain executable plugin code. The repository's
existing isolation contract explicitly forbids exposing those checkouts across
task homes because one task could mutate code another task later executes.
`codex-home/.tmp` therefore remains task-local while the Codex process runs.

After the agent runner returns and its process has been reaped, but before the
daemon reports task completion to the server, remove exact daemon-approved
ephemeral paths including `<task>/codex-home/.tmp`. This ordering prevents a
same-issue successor from being dispatched into the reused env root while
cleanup is running. The directory contains regenerable marketplace/plugin
data, not sessions, credentials, task output, or provider logs. Per-task SQLite
logs and session state remain isolated.

Add `codex-home/.tmp` to the exact daemon-managed artifact paths. Existing
completed task roots therefore shed old regular-directory copies on the normal
artifact-GC schedule. The existing no-symlink rule remains in force, so a
tampered task root cannot redirect cleanup outside its sandbox.

### Spotlight exclusion

Create an empty `.metadata_never_index` file at each daemon workspaces root
before populating a task. The marker is the macOS-native equivalent of a
`.noindex` suffix and covers all current and future task directories under that
root, including crash-orphaned directories. It is harmless on other operating
systems and avoids platform-specific control flow in environment preparation.

Preparation and reuse both self-heal the marker. Failure is logged and remains
non-fatal: inability to optimize indexing must not prevent task execution.

### Existing GC lifecycle

No whole-root retention semantics change:

- done/cancelled issue roots remain eligible after the configured 24-hour TTL;
- crash/timeout roots without metadata remain eligible after the configured
  72-hour orphan TTL;
- completed open-issue roots retain work and session state while exact managed
  caches and configured build artifacts are reclaimed after 12 hours;
- active env-root reservations and symlink guardrails remain authoritative.

## Error handling and safety

- Immediate cache cleanup is best-effort and occurs only after the runner has
  returned. A cleanup failure is logged and does not discard the task result.
- A regular `.tmp` path is removed only after its agent process exits, or by
  artifact GC after completion.
- Artifact GC continues to validate containment and never follows links.
- Spotlight marker creation touches only the configured daemon workspace root.
- No existing host task directory is manually deleted as part of this change.

## Verification

Tests will cover:

- task result reporting observes that exact managed caches have already been
  removed, closing the same-issue successor race;
- cancelled and failed runner outcomes also reclaim the cache when an env root
  was successfully prepared;
- the managed artifact catalog includes `codex-home/.tmp`;
- legacy regular `.tmp` caches are counted and reclaimed while symlinks and
  their targets are preserved;
- fresh preparation and reuse create/self-heal `.metadata_never_index`;
- disk-usage reporting advertises and accounts for the new managed path.

Run the focused execenv, GC, and disk-usage suites, followed by the daemon
package tests and repository formatting/static checks appropriate to the
changed Go packages.
