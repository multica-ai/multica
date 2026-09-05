# Entitlement policy consumer

This package is the mechanical Multica-side consumer of the private Cloud
enforcement-policy endpoint. Commercial inputs stay in Cloud: this package does
not contain plan names, subscription-state mapping, limit values, or policy
switches.

Production wiring has one boundary: setting `MULTICA_CLOUD_URL` connects this
consumer as well as the other managed Cloud clients. An empty URL performs no
HTTP request, issue creation does not count rows, and the autopilot consumer
does not access its quota tables. Self-hosted deployments therefore retain the
unlimited paths. Request timeout and stale grace use bounded code defaults
instead of deployment configuration.

## Contract

The client reads:

- `schema_version`: only version 1 is accepted.
- `policy_revision`: the effective-instruction generation, currently fixed at
  `2` by Cloud and not deployment configuration. Cloud advances it when the
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
- `gates`: effective `off` or `enforce` instructions and parameters. Cloud does
  not expose an `observe` rollout mode; `observe` exists only as Multica's local
  downgrade of an expired cached `enforce` instruction.
- `gates.*.notifications`: an optional, additive delivery policy. The autopilot
  quota consumer recognizes `first_rejection_per_period` and delivers one
  workspace notice for the first rejected execution in the Cloud-defined quota
  period. A malformed notification policy is ignored without invalidating an
  otherwise valid enforcement gate.

Deploy Cloud policy revision 2 before the Multica consumer. The consumer treats
a missing notification policy as notices-disabled while continuing to enforce
the quota. The reverse order is safe because older consumers ignore the
additive notification fields.

Responses tolerate unknown JSON fields for additive compatibility. Unknown
schema/action, malformed fields, missing gates, HTTP failures, and timeouts fail
open.

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

## Issue count limit

The `issue_count` gate limits the total number of rows in a workspace's
`issue` table. It does not use `workspace.issue_counter`: that counter only
allocates monotonically increasing display numbers, while deleting an issue
must release one unit of capacity.

Issue creation resolves the Cloud instruction before opening a transaction.
Inside the transaction, incrementing the workspace issue counter locks the
workspace row, serializing concurrent admission; a bounded `CountIssuesUpTo`
read then admits only when the current row count is below Cloud's limit. A
blocked create rolls back the counter increment with the rest of the
transaction. The usage read is also bounded and samples at most `limit + 1`
rows, with overflow protection for the maximum integer limit.

Entitlement lookup, refresh, expiry, and policy-validation failures are
fail-open: the consumer skips the count and issue creation proceeds. The local
`observe` degradation is likewise normalized to `off` by this consumer; the
shared entitlement decision metric still records the stale observation. Once
a valid `enforce` instruction enters the transaction, ordinary database errors
still abort that transaction rather than creating a partially persisted issue.

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
