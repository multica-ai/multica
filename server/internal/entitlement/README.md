# Entitlement policy consumer

This package is the mechanical Multica-side consumer of the private Cloud
enforcement-policy endpoint. Commercial inputs stay in Cloud: this package does
not contain plan names, subscription-state mapping, limit values, or policy
switches.

Production wiring has one boundary: setting `MULTICA_CLOUD_URL` connects this
consumer as well as the other managed Cloud clients. An empty URL performs no
HTTP request and the autopilot consumer does not access its quota tables. Self-hosted deployments therefore retain the
unlimited paths. Request timeout and stale grace use bounded code defaults
instead of deployment configuration.

## Contract

The client reads:

- `schema_version`: only version 1 is accepted.
- `policy_revision`: the effective-instruction generation, currently fixed at
  `3` by Cloud and not deployment configuration. Cloud advances it when the
  meaning or set of actionable instructions changes, including additive
  instructions; `schema_version` changes only for incompatible wire parsing.
- `subscription_version`: the workspace's monotonic subscription revision. A
  response that moves this revision backwards cannot replace a cached policy
  while it is still usable for fresh or stale decisions. After the bounded
  stale window ends, the cache accepts the current Cloud response so a rollback
  cannot create a permanent retry loop.
- `valid_for_seconds`: the enforcement TTL, measured from local receipt time
  with Go's monotonic clock. It is capped at five minutes. This is authoritative
  for enforcement expiry.
- `valid_until`: diagnostic Cloud wall-clock time only; it is never used to
  extend enforcement.
- `gates`: effective `off`, `observe`, or `enforce` instructions and
  parameters. Cloud chooses the action per gate; `observe` carries the same
  limit and period parameters as `enforce`. Multica also downgrades an expired
  cached `enforce` instruction to `observe` locally (see below).
- `gates.*.notifications`: an optional, additive delivery policy. The autopilot
  quota consumer recognizes `first_rejection_per_period` and delivers one
  workspace notice for the first rejected execution in the Cloud-defined quota
  period. A malformed notification policy is ignored without invalidating an
  otherwise valid enforcement gate.

No deployment order is required for the optional notification policy. The
consumer treats a missing policy as notices-disabled while continuing to
enforce the quota.

Deploy this consumer before Cloud policy revision 3, which is the first to send
`observe`. A consumer released before revision 3 rejects `observe` as an
unknown action and fails the whole policy open to `off`.

Responses tolerate unknown JSON fields and gates for additive compatibility.
Unknown schema/action, malformed fields, a missing `autopilot_runs` gate, HTTP
failures, and timeouts fail open.

## Actions

Every consumer handles the three actions the same way:

| Action | Admits the operation | Records an over-limit attempt |
| --- | --- | --- |
| `off` | yes, without reading quota tables | no |
| `observe` | yes, even past the limit | yes, `observed_count` |
| `enforce` | only within the limit | yes, `enforced_count` |

Over-limit attempts are stored per workspace, gate, and Cloud-defined period
in `entitlement_limit_overage` (`service.EntitlementOverages`). Each row keeps
the latest limit, the peak usage an attempt would have reached, the observed
and enforced counts, and the first and last attempt times, so
`peak_usage - limit_value` is how far a workspace went over. A later admission
inside the limit also syncs a changed limit onto an existing row, so an upgrade
mid-period clears the overage without erasing the history. Rollout reach,
over-limit notices, and grace periods read this table; metrics stay
low-cardinality and never carry workspace IDs. Recording runs after the
admission transaction commits and is best-effort: a failed write is logged and
never changes the admission decision.

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

## Autopilot quota

The client itself has no background goroutine and introduces no startup
dependency; the autopilot consumer owns its policy-neutral accounting and
recovery lifecycle separately. Cloud remains the only place that determines
the effective policy from subscription facts and authoritative limits.

For an enforcing autopilot policy, the first rejected run in a period that
finds at least one workspace owner or admin is delivered once to all such
managers. With no manager the marker stays unset so a later rejection can retry;
after a successful delivery, later rejections create no more Inbox items.

Quota admission and blocked-count transactions commit before a separate Inbox
transaction begins. Inbox inserts and notification-state parsing are therefore
best-effort presentation side effects: failures are logged and the unmarked
first rejection is retried later, but they cannot roll back a run, replace a
quota error with a 500, or erase the blocked count. Issue-less Inbox items are
published only after their own transaction commits. Stored English title/body
remain complete fallback copy for clients and delivery channels without
structured localization; in-app quota views localize from `details`.

Future consumers should depend on the small `Provider` interface. Tests can use
`server/internal/entitlement/entitlementtest.Stub` without Cloud.
