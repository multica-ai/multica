# Codex Capacity Auto-Retry Design

Date: 2026-08-11

## Goal

Automatically retry a Multica Codex task when, and only when, its raw error is
exactly:

```text
Selected model is at capacity. Please try a different model.
```

This removes the manual retry step for transient Codex model-capacity failures
while leaving all other failure behavior unchanged.

## Production Evidence

COM-44 on `multica.fabu.ai` currently demonstrates the gap. Its 43 recorded
runs include 15 failures with the exact raw error above. The latest inspected
failure (`b0b4a98c-01e6-45a7-90ab-d7da6fa800bd`, completed
2026-08-11T01:31:18Z) persisted the legacy reason
`agent_error.model_not_found_or_unavailable` with `attempt=1` and
`max_attempts=2`. Subsequent executions were manual reruns rather than automatic
capacity retries.

## Configuration

The API server reads the deployment-level environment variable
`MULTICA_CODEX_CAPACITY_RETRY_COUNT` at startup.

- The default is `6`.
- The number means additional retries, so `6` permits seven total executions.
- `0` disables only this Codex capacity policy.
- The value must be a base-10 integer from `0` through `2147483646`, leaving
  room for the initial execution in the signed 32-bit task attempt columns.
- An unset variable uses the default. An invalid or negative value logs a
  startup warning and uses the default.
- This setting is authoritative for the special policy. A task's generic
  `max_attempts` value does not reduce or disable it.

Each retry child persists `max_attempts = configured retry count + 1`. This
keeps the execution record self-describing and avoids rows such as
`attempt=3/max_attempts=2`.

## Matching Rules

Capacity retry eligibility is determined from the raw error, not from the
shared failure-reason bucket. The comparison is byte-for-byte equality with the
complete string above.

The policy deliberately does not:

- trim leading or trailing whitespace;
- fold letter case;
- accept prefixes, suffixes, or embedded matches;
- retry other capacity, overload, 429, or rate-limit failures; or
- add the shared `agent_error.provider_capacity_or_rate_limit` reason to the
  generic `retryableReasons` allowlist.

The task-failure classifier may assign the shared capacity reason to this exact
Codex error for reporting. Server-side normalization also upgrades the known
labels emitted by older daemons, including
`agent_error.model_not_found_or_unavailable`, when the raw error is exact. The
raw-error predicate remains the retry gate, so classification cannot broaden
the policy to unrelated failures.

## Architecture And Flow

The implementation reuses the existing server-side
`FailTask -> CreateRetryTask` transaction path.

1. The daemon reports a failed task and its raw error to `FailTask`.
2. `FailTask` classifies and normalizes the reason for persistence.
3. The retry gate evaluates generic retry policy and the dedicated Codex
   capacity policy separately.
4. For an exact capacity match with remaining configured budget, `FailTask`
   creates the retry child in the same transaction that marks the parent
   failed. This error is resume-safe, so the child inherits the existing
   session and work directory when they are available.
5. The child is created without `fire_at`; it is queued and claimable
   immediately. No delay, exponential backoff, or maximum-backoff calculation
   applies.
6. The existing wakeup path notifies the runtime that work is available.

The orphan-recovery path, `MaybeRetryFailedTask`, applies the same predicate to
the parent's persisted raw error and the same configured ceiling. This keeps
normal failure handling and sweeper recovery consistent.

Existing safety boundaries remain in place: automatic retry requires an issue
or chat-session link, and autopilot tasks remain owned by the autopilot
scheduler rather than the task retry loop.

## Service Boundaries

Environment parsing stays at the API composition boundary. The parsed count is
passed through handler configuration and stored on `TaskService`; service code
does not call `os.Getenv`.

The task service owns three small decisions:

- exact raw-error recognition;
- the capacity-specific total-attempt ceiling; and
- selection of immediate retry without `fire_at`.

Generic failure reasons retain their existing allowlist, ceilings, and delays.
In particular, the provider-network three-attempt schedule and its final
five-second delay do not change.

## Failure Reporting

An attempt that successfully creates a retry child remains silent through the
existing retry-pending behavior: it does not emit the user-visible terminal
failure or quick-create failure notification. The retry child reports its own
outcome.

When the configured capacity budget is exhausted, or retry-child creation
fails, the task follows the existing terminal failure path and the error is
shown normally. There is no fallback to another model.

## Deployment And Documentation

The variable is exposed wherever non-secret backend deployment configuration
is declared:

- `.env.example`, with default `6`;
- self-hosted Docker Compose backend environment, forwarding the deployment
  value with default `6`;
- Helm `backend.config` values and backend ConfigMap; and
- the environment-variable documentation, explicitly listed as API-server
  configuration rather than daemon configuration.

No secret handling is required.

## Tests

Focused tests cover:

- exact classification and normalization for current and legacy daemon labels;
- rejection of case changes, surrounding whitespace, prefixes, suffixes, and
  unrelated 429/rate-limit errors;
- environment parsing for unset, positive, zero, negative, malformed, and
  out-of-range values;
- `6` producing six retry children and a total ceiling of seven attempts;
- `0` disabling the special policy;
- generic `max_attempts=1` not disabling an enabled capacity policy;
- capacity retries creating no delay or `fire_at` value;
- generic retry policies retaining their existing budgets and backoff; and
- final exhaustion becoming a normal visible failure while intermediate
  retrying failures remain silent.

The implementation starts with failing focused tests, then runs the affected Go
package tests and the repository's broader verification appropriate to the
changed backend and deployment files.

## Data And Migration Impact

No schema or data migration is needed. Existing `attempt`, `max_attempts`, raw
error, failure reason, parent-task linkage, and `fire_at` fields already express
the required behavior.

## Non-Goals

- Generic provider-capacity or rate-limit retries.
- Model fallback or model selection changes.
- Delayed or exponential retry scheduling.
- Per-agent, per-workspace, or UI configuration.
- Retrying already-terminal historical COM-44 runs.
