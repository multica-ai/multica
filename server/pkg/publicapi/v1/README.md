# Multica Public API v1

This package is the source of truth for the first versioned Public API slice:

- `openapi.yaml` defines the wire contract.
- `routes.go` is the capability ledger used by router and contract tests.
- `types.go` contains DTOs that are deliberately independent from App API
  responses and database models.
- `problem.go` defines the stable error envelope used before and after routing.
- `foundation.go` defines actor, credential, pagination, idempotency, revision,
  risk, audit, and rate-limit vocabulary shared by every future resource slice.

The Plugin API is the first deployed surface. Its installation/callback tokens,
manifest scopes, rate limits, and audit policy narrow the shared resource
contract. Plugin context and storage are marked `plugin_extension` and are not
implicitly part of a future user/PAT surface.

Both surfaces should call the same Go service and handler layer. The Plugin API
must not make an additional HTTP request to the user/PAT API.

Additive fields and endpoints remain in v1. A breaking wire change requires a
new major version and a migration window. New write operations should adopt the
shared `Idempotency-Key` and revision/ETag components rather than inventing
endpoint-specific concurrency rules.

## Foundation rules

- User OAuth and PAT authenticate a member; Plugin installation and invocation
  credentials authenticate either a member-bounded invocation or the
  installation itself. Authentication happens at the surface, before a shared
  resource service runs.
- Collection endpoints use opaque cursor pagination with `limit` (default 50,
  maximum 200) and return `next_cursor`. The migrated comment endpoint keeps
  its existing newest-200 compatibility window until its dedicated pagination
  slice lands; it is not a precedent for new collections.
- Create, trigger, replay, and retry operations must durably deduplicate an
  `Idempotency-Key` (maximum 255 bytes) before the operation is documented as
  idempotent. Merely accepting the header is insufficient.
- Mutable resources expose a revision and ETag. Conditional writes accept
  `If-Match`; stale writes return `revision_conflict`.
- The capability ledger states credential families, scope, risk, audit, and
  rate-limit policy. Plugin requests use the stricter surface profile even when
  the resource DTO and service are shared with a user/PAT request.
- Audit requirements use an explicit lifecycle (`planned` or `enforced`). The
  migrated operations remain `planned` until a durable audit sink is wired;
  the ledger does not imply that a boolean marker performs an audit write.

## Rollout ledger

The migrated slice is Issue read/content update and Comment read/create. Context
and Storage remain Plugin extensions; Hook and invocation delivery remain on
the bridge. Projects, Members, Issue search/create/transition, Agents, Squads,
Skills, Tasks/Runs, and Autopilots are subsequent vertical slices. Each slice
adds its Public contract first, then explicitly opts the safe Plugin subset into
the capability ledger.
