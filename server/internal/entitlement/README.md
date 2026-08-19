# Entitlement policy consumer

This package is the mechanical Multica-side consumer of the private Cloud
enforcement-policy endpoint. Commercial inputs stay in Cloud: this package does
not contain plan names, subscription-state mapping, rollout dates, cohorts,
exemptions, limit values, or kill-switch policy.

Production wiring remains explicit and off by default. Set
`MULTICA_ENTITLEMENT_POLICY_ENABLED=true`,
`MULTICA_ENTITLEMENT_POLICY_URL`, and the independent
`MULTICA_ENTITLEMENT_SERVICE_TOKEN` to enable the client. A disabled client
performs no HTTP request, and the autopilot consumer does not access its quota
tables; self-hosted deployments therefore retain the legacy dispatch path.
Timeout, stale grace, and the emergency down switch are controlled by
`MULTICA_ENTITLEMENT_POLICY_TIMEOUT`, `MULTICA_ENTITLEMENT_STALE_GRACE`, and
`MULTICA_ENTITLEMENT_EMERGENCY_DISABLED`.

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
`off`; it cannot promote a Cloud action. The client itself has no background
goroutine and introduces no startup dependency; the autopilot consumer owns its
policy-neutral accounting and recovery lifecycle separately.

Future consumers should depend on the small `Provider` interface. Tests can use
`server/internal/entitlement/entitlementtest.Stub` without Cloud.
