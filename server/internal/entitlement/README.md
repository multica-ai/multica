# Entitlement policy consumer

This package is the mechanical Multica-side consumer of the private Cloud
enforcement-policy endpoint. Commercial inputs stay in Cloud: this package does
not contain plan names, subscription-state mapping, rollout dates, cohorts,
exemptions, limit values, or kill-switch policy.

PR-2 intentionally has no production caller. `Config.Enabled` defaults to
`false`, a disabled client performs no HTTP request, and all gates return `off`.
Future SaaS wiring must explicitly enable and construct the client; self-hosted
deployments remain disabled.

## Contract

The client reads:

- `schema_version`: only version 1 is accepted.
- `policy_revision` and `subscription_version`: independently monotonic. A
  response that moves either revision backwards cannot replace a cached policy
  while it is still usable for fresh or stale decisions. After the bounded
  stale window ends, the cache accepts the current Cloud response so an
  accidental operator rollback cannot create a permanent retry loop.
- `valid_for_seconds`: the enforcement TTL, measured from local receipt time
  with Go's monotonic clock. It is capped at five minutes. This is authoritative
  for enforcement expiry.
- `valid_until`: diagnostic Cloud wall-clock time only; it is never used to
  extend enforcement.
- `gates`: effective `off`, `observe`, or `enforce` instructions and parameters.

Responses tolerate unknown JSON fields for additive compatibility. Unknown
schema/action, malformed fields, missing gates, HTTP failures, authentication
failures, and timeouts fail open.

## Cache and degradation

The cache is workspace-keyed, LRU-bounded, and collapses concurrent refreshes
for one workspace through `singleflight`. Shared refreshes retain request values
but are detached from the first caller's cancellation; an independent
three-second maximum timeout bounds their lifetime. A fresh entry is returned
without an HTTP call. After its local TTL expires, refresh is attempted. If
refresh fails during the bounded stale grace,
cached `enforce` is downgraded to `observe`; after the grace, the result is
`off`. Stale policy never blocks. A five-second per-workspace retry suppression
also bounds Cloud request rate when an outage returns errors immediately; cold
failures are cached only as `off` and never as policy.

`SetEmergencyDisabled(true)` is the local immediate down switch. It only returns
`off`; it cannot promote a Cloud action. No background goroutine, database
state, migration, or startup dependency is introduced by this package.

Future consumers should depend on the small `Provider` interface. Tests can use
`server/internal/entitlement/entitlementtest.Stub` without Cloud.
