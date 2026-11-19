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
  `4` by Cloud and not deployment configuration. Cloud advances it when the
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
- `features`: paid-feature restrictions, one `{"action": ...}` per feature
  (revision 4). See [Paid features](#paid-features). Each feature is parsed on
  its own, after the rest of the policy: a missing, unknown, or malformed
  feature (including `features` of the wrong JSON type) falls back to `off`
  for that feature only, with reason `feature_missing` or `feature_invalid`,
  and never invalidates the gates.

No deployment order is required for the optional notification policy. The
consumer treats a missing policy as notices-disabled while continuing to
enforce the quota.

Deploy this consumer before Cloud policy revision 3, which is the first to send
`observe`. A consumer released before revision 3 rejects `observe` as an
unknown action and fails the whole policy open to `off`.

Revision 4 (`features`) is safe in either order: an older consumer ignores
`features`, and this consumer treats a policy without it as every feature
`off`.

`gates.members` (revision 5) is optional and safe in either order too: an
older consumer ignores the unknown gate, and this consumer reads a policy
without it, or with a malformed one, as `members` `off` with reason
`gate_missing` or `gate_invalid`. Only that gate falls back; `autopilot_runs`
and the features keep their instructions. See [Member limit](#member-limit).

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

## Refreshing before a refusal

A consumer about to refuse an operation can call `Refresher.RefreshGate`
instead of trusting the cache. It fetches the policy again unless the cached
one arrived within the last five seconds, so a workspace that has just
upgraded is not refused on a policy cached before the upgrade, and repeated
refusals cost at most one Cloud request per workspace per five seconds. A
failed fetch answers exactly like `Gate`. The Cloud client implements it;
`entitlementtest.Stub` does too (`SetRefreshed`).

## Member limit

`members` caps the members of a workspace on a plan with a member limit
(Free). It carries a `limit` and no period: it is a ceiling on headcount, not
a budget that resets. Cloud sends it `off` for paid plans and trials, whose
seats are governed by purchased seat capacity (`server/internal/seatcapacity`).

The handler checks it wherever a member is added
(`server/internal/handler/member_limit.go`):

| Operation | Counted against the limit |
| --- | --- |
| Create an invitation (`POST /api/workspaces/{id}/members`) | members + live pending invitations + 1 |
| Join by share link | the same, or members + 1 when the joiner holds a pending invitation |
| Accept an invitation | members + 1: the invitation already holds its slot |

Guests take no slot and are never checked. A refused invitation acceptance
leaves the invitation pending. Each path counts once before its side effects
(Cloud seat calls, rate-limit budgets), re-reading the policy with
`RefreshGate` before refusing, and again inside the transaction that writes
the member or invitation, under a per-workspace advisory lock taken before any
row lock; the second count decides races for the last slot. `enforce` refuses
with `403 member_limit_reached`. Attempts are recorded in
`entitlement_limit_overage` under gate `members`, grouped by UTC calendar month
since the gate has no period of its own.

Clients read `GET /api/workspaces/{id}/member-limit` (member-readable), which
returns `action` and, while a limit applies, `limit`, `members` and
`pending_invitations`.

## Paid features

A paid feature is restricted for a workspace whose plan does not include it.
Cloud decides which workspaces that is and sends one action per feature; this
consumer never sees plan names. `FeatureProvider` is the only interface
callers use, so a self-hosted license provider can replace the Cloud client
without changing them. A nil provider (no `MULTICA_CLOUD_URL`) leaves every
feature usable.

The rule is the same for every feature:

| Write | `off` | `observe` | `enforce` |
| --- | --- | --- | --- |
| Add to the configuration (create, add a person, widen) | allowed | allowed, recorded | rejected `403 feature_locked`, recorded |
| Reduce or remove it (remove a person, make public, delete) | allowed | allowed | allowed |
| Edit without changing it (rename, same-value write-back) | allowed | allowed | allowed |

Reads never consult a feature. Existing configuration keeps working exactly as
configured, including the access control it defines: a private project stays
visible only to its members after a downgrade. Each feature classifies its own
writes; the handler entry point is `requireFeatureAddition`
(`server/internal/handler/paid_feature.go`), called after the ordinary
permission checks, so a caller refused by those gets the ordinary error.

| Feature | Restricted additions | Always allowed |
| --- | --- | --- |
| `private_projects` | create a private project; make a project private; add someone who is not a member yet, directly or by making them the lead | remove members; make it workspace-visible; delete or archive; set the lead to an existing member, an agent or nobody; other fields; the owner's recovery join |

A stale cached `enforce` is downgraded to `observe` with reason `stale` and
`Downgraded` set, then to `off`, exactly like gates, so a Cloud outage admits additions rather than
blocking them. Cloud failure before any policy was cached is `off`.

Attempts are aggregated per workspace, feature, and UTC day in
`entitlement_feature_attempt` (`service.EntitlementFeatureAttempts`):
`observed_count` (admitted under a configured observe), `degraded_count`
(admitted only because a stale enforce was downgraded), and `enforced_count`
(rejected), with the latest operation, action, reason, policy and subscription
revisions, and first/last attempt times. Admitted attempts are recorded after
the write commits, rejections after authorization; recording is best-effort,
bounded by its own one-second timeout so a contended row cannot hold the
response, and never changes the result. A configured `observe` served from a
stale cache still counts as observed; only a downgraded `enforce` counts as
degraded. Attempts measure demand, not existing
configuration: count existing private projects from the product tables.

Clients read `GET /api/workspaces/{id}/features`, which returns only
`available` (off or observe) or `locked` (enforce) per feature, and render
read-only and upgrade states from it. The server enforces on every write
regardless.
