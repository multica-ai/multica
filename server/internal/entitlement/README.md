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
  `10` by Cloud and not deployment configuration. Cloud advances it when the
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
  quota consumer recognizes two values. `first_rejection_per_period` delivers
  one notice for the first rejected execution in the Cloud-defined quota
  period. `first_rejection_per_limit` (revision 6) delivers one notice for the
  first rejection at each limit within the period, so a limit raised by an
  add-on purchase re-arms the notice. Any other value, or a malformed
  notification policy, disables notices without invalidating an otherwise
  valid enforcement gate.
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
`gates.storage_bytes` (revision 6) is optional the same way; see
[Attachment storage limit](#attachment-storage-limit).

Revision 7 adds the `agent_invocation_members`, `runtime_member_sharing` and
`team_app_accounts` features. It is safe in either order: an older consumer
ignores features it does not know, and this consumer reads a policy without
one as that feature `off` with reason `feature_missing`.

`gates.dashboard_history_days` (revision 9) is optional like `members`: a
missing or malformed gate, including a `limit` outside 1–365 or one carrying a
period, is `off` with reason `gate_missing` or `gate_invalid` for that gate
alone. See [Dashboard history limit](#dashboard-history-limit).

`gates.app_tool_calls` (revision 10) is optional like `members`: a missing or
malformed gate, including one without its period, is `off` with reason
`gate_missing` or `gate_invalid` for that gate alone. See
[Monthly app call limit](#monthly-app-call-limit).

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

`FreshRefresher.RefreshGateNow` fetches without the reuse window; concurrent
callers for a workspace share one fetch. The autopilot quota calls it right
before rejecting a run: the admission transaction sees the quota is full on
the cached policy, leaves the transaction, fetches the current policy and runs
the admission once more on it. A limit Cloud raised moments ago, such as an
add-on Stripe's webhook confirmed before the payer returned to the app, admits
the next run on every instance. Each rejection costs one Cloud request.

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

## Attachment storage limit

`storage_bytes` caps the attachment bytes a workspace holds, on every plan:
Cloud sends Free's flat limit or the paid plan's per-seat limit. It carries a
`limit` and no period. `service.StorageQuota`
(`server/internal/service/storage_quota.go`) checks it.

Usage is not stored: it is `SUM(size_bytes)` over the workspace's attachment
rows (`SumWorkspaceAttachmentBytes`, an index-only scan on
`idx_attachment_workspace_size`), so cascade deletes are reflected at once.
Every attachment row counts; thumbnails are not rows. `off` reads nothing.
The sum has its own timeout (`DefaultStorageSumTimeout`, 300 ms); a timeout or
error admits the upload and is counted in `multica_storage_quota_sum_total`.

| Path | Checked as |
| --- | --- |
| `POST /api/upload-file` with a workspace | used + file size, before the object is written |
| Create a sub-issue from a comment (copies the source attachments) | used + copied bytes, before the sub-issue or any copy exists |
| Files received through a messaging channel | recorded after they are stored, never refused |

`enforce` refuses with `403 storage_limit_reached`, `used_bytes` and
`limit_bytes`, after re-reading the policy with `RefreshGate`. `observe`
admits and records. Reads, downloads, thumbnails and deletes are never
checked. It is a soft limit: no lock is taken, so uploads in flight when a
workspace crosses its limit all pass. Attempts are recorded in
`entitlement_limit_overage` under gate `storage_bytes`, grouped by UTC
calendar month.

Clients read `GET /api/workspaces/{id}/storage-usage` (member-readable), which
returns `action` and, while a limit applies, `used_bytes` and `limit_bytes`;
`reached` is present only under `enforce`.

## Monthly app call limit

`app_tool_calls` caps the Connected Apps tool calls a workspace sends to
Composio in a Cloud-defined period (a UTC calendar month). Like
`autopilot_runs` it carries a `limit`, the period, and under `enforce` a
`first_rejection_per_limit` notification policy. `service.AppToolCallQuota`
(`server/internal/service/app_tool_call_quota.go`) checks it; the gateway
calls it through `connectedapps.CallQuota`.

Usage is a counter, `app_tool_call_quota_period.used_count` per workspace and
period, never a `COUNT` over `app_tool_call`. One conditional upsert counts a
call right before it is sent to Composio: under `enforce` it only increments
below the limit, so concurrent calls cannot take a workspace past it; under
`observe` it always increments. A call that then fails upstream still counts,
as Composio bills it.

| Counted | Not counted |
| --- | --- |
| `apps_run_tool` calls the gateway sends to Composio (`toolRun`) | blocked calls, per-minute rate-limited calls, refused calls themselves |
| Confirmed calls when the approval executes them (`executeApproved`) | pending confirmations, meta tools (list, search, get tool, connection requests), Remote MCP apps |

Team and personal accounts both count toward the workspace that started the
run. `enforce` refuses past the limit only after re-reading the policy with
`RefreshGateNow`, so a workspace that upgraded or added seats moments ago is
admitted. A cached policy whose period has already ended (the first minute of
a month) is re-read the same way before counting a call or answering Billing;
if Cloud still sends the old period, the call is admitted without counting and
Billing shows no limit for that moment. The agent gets a tool error with the count, the limit and the reset
time; the call is logged in `app_tool_call` as `quota_exceeded`, and a refused
confirmed call fails its request. Owners and admins get one
`app_tool_calls_quota_exceeded` Inbox item per limit and period, also when
the limit returns to one already notified; a delivery with no recipient leaves
the marker unset for the next refusal. Attempts past
the limit are recorded in `entitlement_limit_overage` under gate
`app_tool_calls`. `off`, and every self-hosted deployment, touches no table;
the per-account per-minute limit applies regardless.

Clients read `GET /api/workspaces/{id}/app-call-usage` (member-readable),
which returns `action` and, while a limit applies, `used`, `limit`,
`period_start` and `reset_at`; `reached` only under `enforce`. Owners and
admins also get `members`: the period's calls per person who started the run
or approved the call (`user_id` null for runs no person started), read from
the `app_tool_call` rows whose `metered_period_start` is the period. The
quota returns the period it counted a call in and the gateway records it, so
the breakdown matches the counter even for a call that finishes after the
month turned. Refused calls, an approved call that fails its checks before
running, Remote MCP calls and every call while the gate is off have none.

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
permission checks, so a caller refused by those gets the ordinary error. A
handler that can only tell whether its write adds while holding row locks
reads the decision first (`paidFeature`) and decides on the locked state, so
it never waits on Cloud under a lock.

| Feature | Restricted additions | Always allowed |
| --- | --- | --- |
| `private_projects` | create a private project; make a project private; add someone who is not a member yet, directly or by making them the lead | remove members; make it workspace-visible; delete or archive; set the lead to an existing member, an agent or nobody; other fields; the owner's recovery join |
| `guests` | invite a guest; make a member a guest; share another project with a guest | remove a guest; narrow a guest's projects; make a guest a member; accept an invitation sent while the plan included guests |
| `agent_invocation_members` | create an agent that specific members may invoke (including duplicating one and the CLI `--public-to-member`); add someone to the list; switch from "only me" or "whole workspace" to specific members | remove someone; switch to "only me" or "whole workspace" (an emptied list is the whole workspace); re-save the same list; other fields; invoking the agent |
| `runtime_member_sharing` | add someone to a private runtime's `shared_with`, including switching from "only me" or public to specific members (decided on the locked runtime row) | remove someone; make it public or "only me"; re-save the same list; using the runtime |
| `team_app_accounts` | connect a team account for an app that has none, including after a disconnect (checked when the connect starts and again in the OAuth callback before the account is written); give an agent a team-account app or switch one from a personal account; raise an agent's level on a team account; set a team rule to a value that is neither its current value nor the default | reauthorize an app's existing team account; disconnect; remove an app from an agent; lower a level; restore default team rules (Allow all actions); personal accounts and rules; gateway calls, including existing `block` rules |

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

## Dashboard history limit

`dashboard_history_days` caps how many calendar days back the workspace
dashboard can read. Its `limit` is a day count (1–365) and it has no period.
Cloud sends it for every known plan: Free, Pro and Business (trials included)
each get their own configured day count.

Only `enforce` limits anything (`server/internal/handler/dashboard_history.go`).
The six `/api/dashboard/*` endpoints then cut their cutoff to the first of the
most recent N calendar days in the viewing time zone, whatever `days` asked
for, including the date series' extra day of headroom and the days a weekly
chart fetches to fill its leftmost week, and send the limit in
`X-Dashboard-History-Limit-Days`. `off`, `observe` (including a downgraded
stale `enforce`, marked `Downgraded` on the `Decision`) and self-hosted
deployments read up to the request maximum of 371 days with no header. The
runtime usage endpoints keep their own 365-day maximum and no plan limit.
The dashboard's `365d` range follows the frontend flag
`dashboard_history_365d`: published on by default, and
`FF_DASHBOARD_HISTORY_365D=off` hides the range should the year-long queries
be too slow for a deployment.

`GET /api/workspaces/{id}/features` reports `limits.dashboard_history_days`
only under `enforce`, so an observed rollout is invisible to users.

Reads of `GET /api/dashboard/usage/by-agent`, the one dashboard endpoint
fetched once per page load or range change with the range the viewer picked,
are aggregated per workspace, UTC day, requested days and returned days in
`dashboard_range_view` while Cloud is connected: view count, reads past the
limit admitted under a configured observe, admitted because of a downgrade,
and cut short under enforce, with the latest limit, action and reason. The
write runs off the request path with its own 500 ms timeout; a failure drops
the row and never changes the response. It selects its workspace row under
`FOR KEY SHARE`, so a write racing workspace deletion either waits for it and
inserts nothing or is removed by it. Counts are read requests, including
the dashboard's periodic refresh, so use distinct workspaces for reach.
