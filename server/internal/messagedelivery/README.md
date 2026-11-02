# Labrastro Message Delivery — Backend Contract (OL-25 + OL-27 + OL-29)

Feishu participants without platform membership converse with the designated
agent under explicit integration consent. Bound workspace members use the
upstream member flow and their own identity. The agent uses normal
tools to summarize feedback under its own identity. Authorization, recovery and
legacy-feedback retirement are defined in [FEEDBACK-CONTRACT.md](FEEDBACK-CONTRACT.md).
Upstream sync points are listed in [UPSTREAM-ADAPTATION.md](UPSTREAM-ADAPTATION.md).
Group and message-anchor picker APIs are defined in [TARGET-DISCOVERY-CONTRACT.md](TARGET-DISCOVERY-CONTRACT.md).

This is the API/contract reference for the standalone result-delivery module
("自动化结果投递" + "个人收件箱与团队事件投递"): automation runs finish,
personal inbox items arrive and team events happen → saved rules push the
content to Feishu → deliveries and receipts are queryable and retryable.
Frontend work MUST read this document and must not guess field names,
statuses or error codes. Code lives in:

| Path | Responsibility |
| --- | --- |
| `server/internal/messagedelivery/` | decisions, content normalization, send worker, compensator |
| `server/internal/handler/labrastro_message_delivery.go` | HTTP surface (automation scope, OL-25) |
| `server/internal/handler/labrastro_message_sources.go` | HTTP surface (personal/team scopes, OL-27) |
| `server/internal/notify/` | shared notification taxonomy (inbox types, preference groups, team source whitelist) |
| `server/internal/integrations/lark/labrastro_delivery.go` | Feishu proactive send (open_id / chat_id / topic reply + fixed UUID) |
| `server/cmd/server/labrastro_messaging.go` | the single assembly point (sender selection, EventBus wakeup) |
| `server/pkg/db/queries/labrastro_message.sql` | all SQL; generated code via `make sqlc` |
| `server/migrations/`: the `labrastro` stems from `452_labrastro_message_tables` through `478_labrastro_feedback_retirement`, plus `9010_labrastro_message_delivery_run_index` through `9015_labrastro_message_source_watermark` (see [SCANNING.md](SCANNING.md)) | tables + concurrent indexes |

## Concepts

- **Route** (`labrastro_message_route`) — a saved rule: one automation
  (autopilot), one bot installation, one target, a condition and a content
  mode. Writes are revision-guarded (`expected_revision`); a stale save is a
  409, never a silent overwrite. Only enabled rules decide, and only for runs
  completing at/after the rule's `effective_from` (set at creation, reset at
  every enable — re-enabling never backfills the disabled window).
- **Delivery** (`labrastro_message_delivery`) — one immutable DECISION per
  `(source event, normalized target)`, keyed by `dedup_key`
  (`run:<run_id>:<installation_id>:<target_key>`). Created exactly once per
  key across concurrent enqueuers and replicas. Content and target are frozen
  snapshots at decision time; later rule edits never rewrite an enqueued
  message.
- **Receipt** (`labrastro_message_receipt`) — one row per message shard with
  a FIXED `send_uuid` (idempotency key) and the platform's
  `external_message_id` (unique per installation, traceable back to the
  source for the feedback stage).

### Delivery statuses

| status | meaning |
| --- | --- |
| `queued` | decided, waiting to send |
| `sending` | lease held while checking gates and sending shards; a crashed claim is conservatively recovered as `uncertain` |
| `sent` | every shard accepted, receipts recorded |
| `failed` | definitive failure; `error_code` says why |
| `uncertain` | a dialed send may have landed (lost response / lost receipt), or a crashed claim left no reliable verdict; requires manual verification, and retry remains subject to current send gates and the disable fence |
| `cancelled` | further sending stopped by route, source or installation gates; earlier shards may already have been accepted |
| `suppressed` | condition mismatch (incl. skipped runs) or unknown historical origin; recorded so the source is not rescanned |

### Source semantics

- `run_only` (`source_kind="run_only"`): delivers the run's terminal state.
  `content_mode=with_output` includes the final output **only** — the
  extracted `output` string from the task completion payload. Session ids,
  work dirs, branch names and the raw result JSON are never sent, logged, or
  returned. Empty output renders `(no report body)`. Failed runs show a
  curated `reason_code` when one exists, never the raw error text.
- `create_issue` (`source_kind="create_issue"`): delivers the run outcome,
  the issue status frozen at settlement, and a task link
  (`{appURL}/{workspaceSlug}/issues/{IDENTIFIER}` — workspace-scoped per the
  frontend conventions). The run's own dispatch chain determines success or
  failure; a completed dispatch succeeds even when the issue is `blocked`.
  The atomic terminal write freezes `first_terminal_status` on the run;
  a run without that frozen signal — completed before it existed —
  degrades to wording that does NOT name a possibly-later status. Later
  issue status changes are not re-delivered by this module. The source kind
  comes from the run's persisted links, never the autopilot's current
  configuration. No report body is attached — attaching requires an
  explicit delivery-comment anchor (feedback stage).
- `unknown`: no issue link, task link (in either direction), or output evidence
  proves the historical mode. The decision is `suppressed`, with
  `error_code="source_unresolved"`, empty content and `shard_total=0`. Editing
  the current automation mode cannot change that historical decision.
- Manual **test sends** (`source_kind="test_send"`) run the real send path
  synchronously with a synthetic message and are recorded like deliveries.
  Their resolved human actor is stored in `requested_by` and reauthorized
  before sending, including after recovery. `source_ref` remains a locator,
  never a grant. Pre-upgrade diagnostic rows without an actor are cancelled
  on recovery; create a new test send to authorize a new diagnostic request.
- Send failures NEVER re-run or re-queue an automation; execution modes,
  quotas and the execution state machine are untouched.

### Target types

| type | saved fields | send addressing |
| --- | --- | --- |
| `member` | `target_user_id` | DM via `receive_id_type=open_id`, resolved live from `(workspace, installation, user)` binding immediately before every send |
| `group` | `target_chat_id` | `receive_id_type=chat_id` |
| `topic` | `target_chat_id` + `target_message_id` (+ optional `target_thread_id`) | reply to the anchor message with `reply_in_thread=true` |

Member binding is installation-precise. The legacy "most recent binding of
the channel" query is deliberately NOT used: with multiple bots in one
workspace it picks the wrong app.

### Approved external targets (workspace-admin consent)

Group and topic targets additionally require an ACTIVE approval scoped to
the exact **(workspace, automation, bot, target)** scope — approving
automation A's outbound target is never borrowed by automation B. Approval
and revocation are workspace owner/admin acts. All three approval endpoints
judge the already-resolved acting human, including when a task runs on someone
else's machine; `approved_by` records that same human. Approving
consents to sharing that automation's rule-permitted result content with
that target, nothing more. The route save and EVERY execution path (enable,
test-send, worker send, retry) check the approval — send-time checks key on
the delivery's FROZEN target identity. Revocation cancels queued sends;
platform-accepted requests are not recallable. Revocation identifies the exact
approval ID and its installation, and commits queued cancellation in the same
transaction. A missing/already-revoked approval returns 404; cancellation
failure rolls back the revoke instead of claiming success. Member targets stay out of
the approval table: their binding proves address ownership and the route
authorizer's source permission covers the sharing decision.

The current repair contract (round-2 review response, four boundaries,
acceptance matrix A1–E1) ships as `REPAIR-CONTRACT-v1.md` in this directory.

### Target verification (save-time AND send-time)

External group/topic targets are verified against the live platform before
a route referencing them may be SAVED and again before every send:

- **group**: the pinned bot must be able to see the chat (chat-info probe).
- **topic**: the anchor message must provably live in the declared chat; the
  VERIFIED chat id is what the route stores and sends against, so a
  declared-but-foreign chat can never redirect a topic reply. The
  anchor→chat relationship is re-proved at send time and a moved anchor
  fails the delivery with `route_topic_anchor_mismatch`.

A deployment without the channel transport has no way to verify and FAILS
CLOSED: group/topic saves return `route_target_unverifiable`. A
definitive platform refusal fails the save with `route_target_unreachable`.
An optional later test-send does not substitute for this check.

### Continuous authorization

A rule spends the authority of the member who LAST SAVED it (`updated_by`).
The send gate re-checks on every claim that this member is still a
workspace member AND still holds write on the source automation (owner/
admin, creator, or granted collaborator — the same predicate the HTTP gate
enforces at edit time). When authorization is lost, the delivery is
recorded as `cancelled` with `route_authorization_lost` and the rule
delivers nothing further until someone authorized edits it (which
re-stamps `updated_by` and re-arms the rule).

## HTTP API

All endpoints live under the authenticated workspace-scoped group and are
gated by the SAME autopilot write gate as the rest of the automation surface
(`requireAutopilotWrite` → an agent caller acts for its run's originator; no
resolvable human → 403 `autopilot_no_originator`). Config author, automation
authorizer and message recipient are distinct identities; `created_by` /
`updated_by` record the config author.

| Method & path | purpose |
| --- | --- |
| `POST /api/autopilots/{id}/message-approved-targets` | owner/admin approves one verified external target for this source |
| `GET /api/autopilots/{id}/message-approved-targets` | owner/admin lists active approvals |
| `DELETE /api/autopilots/{id}/message-approved-targets/{targetId}` | owner/admin revokes exactly one approval; stops queued sends |
| `GET /api/autopilots/{id}/message-routes` | list rules |
| `POST /api/autopilots/{id}/message-routes` | create rule (revision 1) |
| `PUT /api/autopilots/{id}/message-routes/{routeId}` | update rule, `expected_revision` required |
| `POST /api/autopilots/{id}/message-routes/{routeId}/enable` | enable/disable, `{"enabled":bool,"expected_revision":N}` |
| `DELETE /api/autopilots/{id}/message-routes/{routeId}` | delete rule (queued sends cancelled) |
| `POST /api/autopilots/{id}/message-routes/{routeId}/test-send` | real-path reachability check |
| `GET /api/autopilots/{id}/message-deliveries?run_id=&status=&limit=&offset=` | records page, optionally scoped to one run (projection, no snapshots) |
| `GET /api/autopilots/{id}/message-deliveries/{deliveryId}` | detail incl. snapshots + receipts |
| `POST /api/autopilots/{id}/message-deliveries/{deliveryId}/retry` | re-queue a `failed`/`uncertain` delivery |

### Approval — request / response

`POST /api/autopilots/{id}/message-approved-targets`:

```json
{"installation_id":"<uuid>","target_type":"group","target_chat_id":"oc_test"}
```

201 returns `{"approved_target":{...}}`. The row fields are `id`, `workspace_id`,
`autopilot_id`, `installation_id`, `target_key`, `target_type`, `approved_by`,
`approved_at`, `revoked_at` (null while active). Topic approval additionally
requires `target_message_id` and verifies its chat. GET returns
`{"approved_targets":[]}`. DELETE returns
`{"revoked":true,"cancelled_deliveries":1}` only after commit. This does not
recall an already-accepted shard.

### Create rule — request / response example

`POST /api/autopilots/{id}/message-routes`

```json
{
  "installation_id": "9f1c…",
  "target_type": "member",
  "target_user_id": "0d4b…",
  "conditions": "success",
  "content_mode": "with_output",
  "enabled": true
}
```

`201`:

```json
{
  "route": {
    "id": "c1ab…",
    "workspace_id": "bc8c…",
    "autopilot_id": "0a77…",
    "installation_id": "9f1c…",
    "channel_type": "feishu",
    "target_type": "member",
    "target_user_id": "0d4b…",
    "target_chat_id": null,
    "target_message_id": null,
    "target_thread_id": null,
    "target_key": "member:0d4b…",
    "conditions": "success",
    "content_mode": "with_output",
    "enabled": true,
    "revision": 1,
    "created_by": "55e0…",
    "updated_by": "55e0…",
    "effective_from": "2026-09-08T02:30:00Z",
    "last_disabled_at": null,
    "created_at": "2026-09-08T02:30:00Z",
    "updated_at": "2026-09-08T02:30:00Z"
  }
}
```

Group target: `"target_type":"group"`, `"target_chat_id":"oc_…"`.
Topic target: `"target_type":"topic"`, `"target_chat_id":"oc_…"`,
`"target_message_id":"om_…"`.

### Run-filtered records and pagination (OL-26 R3)

`run_id` is an optional UUID filter on the persisted delivery's `run_id`.
Omit the parameter to list all sources, including `test_send` rows whose
`run_id` is null. A supplied empty or malformed UUID returns 400 with
`{"error":"invalid run_id"}` through the existing UUID validator. The filter
intersects `status` and the existing workspace/autopilot scope; it never
changes `requireAutopilotWrite` or grants access to another source.

Every successful list response now includes `applied_run_id`: the canonical
UUID actually applied by SQL, or null when no run filter was requested.
The echo is present even when no delivery matches (including a run outside
this automation, a nonexistent run, or an exhausted page). These cases all
return an empty `deliveries` array without disclosing another source.

```http
GET /api/autopilots/<autopilot-id>/message-deliveries?run_id=019e0123-4567-7000-8000-0123456789ab&limit=100&offset=0
```

```json
{
  "deliveries": [],
  "limit": 100,
  "offset": 0,
  "applied_run_id": "019e0123-4567-7000-8000-0123456789ab"
}
```

For a run-scoped request, clients must compare the echo with the requested
run UUID before interpreting an empty page as "no deliveries". Older servers
may ignore `run_id` and return 200 without an echo; a missing, null, malformed
or mismatched echo cannot confirm support. The shared client's `runId` option
encodes `run_id`; its response schema preserves `applied_run_id` and normalizes
missing/malformed echoes to null. Frontend run queries must carry the run ID,
status and page position in their cache identity and render an unsupported
state when the echo cannot confirm the requested filter.

Pagination retains `limit` (1–200, default/fallback 50) and `offset` (default
0). Use fixed page lengths, for example 100 with offsets 0/100/200, rather
than increasing the limit past 200. Rows sort by `created_at DESC, id DESC`,
including ties, and a page shorter than its returned `limit` ends the current
listing. This is offset pagination, not a snapshot: concurrent new decisions
can shift later pages, so clients should deduplicate IDs and refresh from the
first page. This extension introduces no migration or sending side effect.

### Delivery record example

`GET …/message-deliveries` page items:

```json
{
  "id": "e452…",
  "workspace_id": "bc8c…",
  "route_id": "c1ab…",
  "route_revision": 3,
  "autopilot_id": "0a77…",
  "run_id": "77bd…",
  "source_kind": "run_only",
  "status": "sent",
  "attempts": 1,
  "next_attempt_at": "2026-09-08T02:30:01Z",
  "error_code": null,
  "last_error": null,
  "shard_total": 1,
  "installation_id": "9f1c…",
  "target_key": "member:0d4b…",
  "delivered_at": "2026-09-08T02:30:02Z",
  "first_attempt_at": "2026-09-08T02:30:01Z",
  "created_at": "2026-09-08T02:30:01Z",
  "updated_at": "2026-09-08T02:30:02Z"
}
```

Detail adds `content_snapshot` (`{"text","summary","run_status","has_output","link"}`),
`target_snapshot` (`{"target_type","channel_type","installation_id","user_id","open_id","chat_id","message_id","thread_id"}`),
`source_ref` (feedback anchors: `run_id`, `execution_mode`, `issue_id`,
`issue_identifier`, `issue_status`) and `receipts`:

```json
{
  "receipts": [
    {
      "id": "aa31…",
      "delivery_id": "e452…",
      "workspace_id": "bc8c…",
      "installation_id": "9f1c…",
      "shard_index": 0,
      "shard_total": 1,
      "send_uuid": "5c07…",
      "external_message_id": "om_9f2…",
      "created_at": "2026-09-08T02:30:01Z",
      "updated_at": "2026-09-08T02:30:02Z"
    }
  ]
}
```

### Error codes (stable strings; the human sentence is not contract)

| code | HTTP | when |
| --- | --- | --- |
| `route_invalid` | 400 | malformed rule payload; message names the field |
| `route_installation_invalid` | 400 | installation missing from workspace or revoked |
| `route_member_not_bound` | 400 | member target has no binding on this installation |
| `route_target_not_member` | 400 | member target is not a workspace member |
| `route_not_found` | 404 | route id not in this workspace/automation |
| `route_revision_conflict` | 409 | stale `expected_revision` |
| `route_already_exists` | 409 | equivalent rule already exists |
| `route_target_unverifiable` | 400 | no channel transport is wired to verify the external target; nothing was saved |
| `route_target_unreachable` | 400 | the bot definitively cannot reach the declared chat; nothing was saved |
| `route_topic_anchor_mismatch` | 400 | the anchor message provably lives in a different chat; nothing was saved |
| `route_disabled` | 409 | test-send on a disabled rule |
| `delivery_not_found` | 404 | delivery id not in this workspace/automation |
| `delivery_not_retryable` | 409 | retry on a status other than `failed`/`uncertain` |
| `message_target_admin_required` | 403 | the acting human is not an owner/admin (approval APIs) |
| `authorization_lost` | 403 | the acting human lost source permission before configuration commit |
| `source_unavailable` | 409 | the source was archived or removed before configuration commit |
| `autopilot_no_originator` / `autopilot_forbidden` | 403 | from the shared autopilot gate (see its docs) |

Delivery `error_code` values recorded by the pipeline:
`route_disabled`, `route_deleted`, `source_archived`, `source_missing`,
`condition_mismatch`, `member_unbound`, `installation_revoked`,
`installation_missing`, `send_rejected`, `send_transient`,
`sender_unavailable`, `attempts_exhausted`, `lease_expired`,
`send_ambiguous`, `route_target_unreachable`, `route_topic_anchor_mismatch`,
`route_authorization_lost`, `route_target_not_approved`, `source_unresolved`.

## Reliability guarantees and boundaries

- **One decision per (source, target).** Unique index on `dedup_key`; the
  event wakeup, the compensator and other replicas race safely.
- **Event wakeups decide before sending.** `autopilot:run_done`, `inbox:new`,
  `activity:created` and `comment:created` notify the decision scanner through
  a coalescing channel. The publisher does no database work. The scanner runs
  one bounded pass over persisted sources, including terminal runs, and new
  decisions wake the send pool. A lost event or backlog beyond the 200-pair run
  batch is recovered by the periodic scan.
- **Known unsent failures retry.** Gate, approval, binding, lease-read and
  receipt-claim database errors, failed target verification without a definitive
  refusal, and cancellation before dialing are transient. Regular deliveries
  return to `queued` with backoff and fail at the configured attempt limit;
  they do not become `uncertain` if the outcome can be persisted. A database
  outage that also prevents the retry write leaves the claim in `sending`;
  lease expiry then conservatively recovers it as `uncertain`. Definite
  source/consent loss cancels, and definite invalid destinations fail. The
  synchronous diagnostic send records transient failures as `failed`, available
  for explicit retry.
- **Manual retries retain the original decision time.** For every scope,
  including run routes, disabling a route (explicitly or by editing it) or
  revoking its installation fences all decisions made at or before that stop.
  Re-enabling the route or restoring the installation does not clear the fence.
  A manual retry of an older `failed` or `uncertain` delivery is accepted into
  `queued`, then cancelled before sending with `route_disabled` and
  `delivery predates the last route disable`. No additional shard is sent;
  any previously accepted shards still require manual verification. This also
  applies to retrying a failed notification after reinstalling the bot.
- **Shared Feishu classification.** Sending, target verification and discovery
  share one provider error taxonomy. Rate-limit codes `230020`, `99991400` and
  `99991403` (and HTTP 429) are transient; installation credential lookup
  database errors are also transient. Missing/revoked installations and local
  credential decryption failures remain definitive failures. Codes `230006`,
  `232004`, `232025` and `232034` remain permanent refusals for sending and
  verification, while discovery reports them as unavailable. Token-acquisition
  transport/provider failures still use the existing conservative ambiguous
  send classification; the local-credential guarantee does not cover them.
  Internal database errors are logged; delivery records use generic descriptions.
- **Bounded shutdown.** Worker cancellation stops new claims and shards. A
  shard already entering the sender has a detached 20s context; its receipt and
  the delivery outcome each have a separate 5s write budget. The server joins
  delivery workers for 30s. Accepted shards retain their receipts; a cancelled
  remaining shard is retried without replaying accepted shards. A genuine
  post-dial timeout or lost receipt still becomes `uncertain`; a process crash
  remains subject to conservative lease-expiry recovery. Deployment stop grace
  must allow the full drain budget; an earlier forced kill can still leave an
  unresolved claim for lease-expiry recovery.
- **Compensation.** A scanner pass (default 30s) (1) parks expired send
  claims as `uncertain`, (2) feeds three PERSISTED source classes whose
  automation run missed the terminal event to the EXISTING sync logic
  (`SyncRunFromTask`, `SyncRunFromIssue`, `SyncRunFromLinkedIssueTask` — no
  second state machine) and (3) decides every persisted terminal run that
  still lacks a decision for an enabled rule target. Each scanner holds a
  persistent per-scanner cursor (compare-and-set generation, immutable-id
  keyset, fixed per-cycle bound and per-tick row budget): a tick resumes
  where the last stopped, a completed cycle restarts from the set's
  beginning. `cycle_upper_id` is frozen once; downtime does not change it.
  Each fully processed page advances with a generation CAS. Either direction
  of a committed task/run link is sufficient. Historical linked-task failures
  are rechecked against the latest persisted attempt; a completed successor
  is never replayed as an old failure. A persisted terminal issue is handled
  by its normal sync path before any historical task failure. Thus late-committing sources and pages of long-running candidates
  can never permanently starve anyone.
- **Workspace deletion.** Approval, configuration, decision and receipt inserts share a short
  transaction with a `FOR SHARE` lock on the workspace row (the delete flow
  takes the same row `FOR UPDATE` before sweeping), so a stale snapshot can
  never commit content-bearing rows into a deleted workspace.
- **Test sends.** A synchronous test send carries a bounded lease like a
  claimed row and writes its outcome on a detached context: an interrupted
  test send is recovered by the expiry sweep to `uncertain` and is then
  available for manual retry subject to the same send gates and disable fence
  — it can never strand in `sending`.
- **Exactly-once send is bounded, not absolute.** Retries replay the fixed
  per-shard `send_uuid`, carried in the request BODY per Lark's
  `CreateMessageReqBody` (the platform's dedup window is finite, ~1h).
  Beyond the verifiable window an `uncertain` outcome requires human
  verification; a retry can still be cancelled by the disable fence without
  resolving whether the earlier send landed. Shards already carrying an
  `external_message_id` are never re-sent, and a worker re-validates its lease
  against the database clock before starting each new shard. All result writes
  require the same live token and expiry.
  Accepted late receipts can be recorded on a bounded detached context, but
  cannot renew a lease, overwrite a new owner or create an automatic retry.
- **No side effects on execution.** Delivery never triggers runs, consumes
  quota or mutates the execution state machine.
- **Cleanup hooks.** Rule disable/delete and bot revocation cancel queued
  sends; workspace deletion sweeps routes, deliveries and receipts; the
  worker re-reads route/source/installation state on every claim, so a
  delete racing a send cancels or fails the delivery explainably instead of
  misdelivering.
- **Feishu integration boundary.** Sends go through `lark.DeliverySender` →
  the optional `DeliveryAPIClient` capability (open_id / chat_id /
  reply+thread, fixed uuid). No existing interface was widened; deployments
  without `MULTICA_LARK_SECRET_KEY` record deliveries that fail with
  `sender_unavailable`.

## OL-27 sources: personal inbox and team events

Stage 2 of the parent plan widens the pipeline's SOURCE SCOPE. The delivery
machinery — frozen decisions, the lease worker, receipts, retries and cursor
advancement — is shared with OL-25. Source adapters own event selection and
permission checks; destination verification and the retry transition have
one implementation for all scopes.

### Route source scopes (`labrastro_message_route.source_kind`)

| scope | source record | target shape | who configures |
| --- | --- | --- | --- |
| `run` (default) | terminal `autopilot_run` | member / group / topic | automation write gate (OL-25 surface) |
| `inbox` | the route owner's own `inbox_item` rows | member (their OWN DM), forced | the acting member themselves |
| `activity` | `activity_log` with action `status_changed` / `assignee_changed` | group / topic only | workspace owner/admin |
| `comment` | `comment` with type `comment` (plain comments only) | group / topic only | workspace owner/admin |

Hard rules:

- **No virtual autopilot.** Non-run routes leave `autopilot_id` NULL; a
  personal or project id never impersonates an automation id (the
  `labrastro_message_route_source_shape` check enforces this at the row
  level). A team route to a member DM is unrepresentable — team events fan
  out to shared destinations only, the personal inbox is the DM surface.
- **Identity resolution.** Every endpoint on the OL-27 surface resolves the
  ACTING member through the same seam as the automation surface (`MUL-7108`
  path): a member acts for themselves, an agent for its run's originator,
  no resolvable human → 403 `message_no_originator`.
- **Self-only inbox.** An inbox route's recipient is ALWAYS the resolved
  acting member; the caller's `target_user_id` is never consulted.
  Managing another member's personal route → 403 `route_not_self`, admins
  included. Lists return only the acting member's inbox rules. An unfiltered
  list additionally includes team rules for current owners/admins; explicitly
  requesting an activity/comment list without that role returns 403.
- **Team configuration is admin-only.** Team routes and team target
  approvals require workspace owner/admin (403
  `message_target_admin_required`). An ordinary member managing their own
  notifications never gains team outbound permission.
- **Continuous authorization.** Every send re-reads the world before
  dialing: personal sends re-check the recipient's membership and CURRENT
  mute state; team sends re-check the authorizing member (`updated_by`)
  still holds owner/admin, the source record still exists, and the issue
  still matches the delivery's FROZEN project range. Editing the live route
  does not broaden an existing decision's authority. Losing authorization cancels
  the queued send (`route_authorization_lost` / `condition_mismatch` /
  `recipient_muted`), never misdelivers.

### Canonical sources and dedup

| scope | source record identity | whitelist |
| --- | --- | --- |
| `inbox` | `inbox_item.id`, `recipient_type='member'` | filterable by the route's `event_types[]` (validated against the shared `notify` catalog) |
| `activity` | `activity_log.id` | actions `status_changed`, `assignee_changed` only |
| `comment` | `comment.id` | type `comment` only — `status_change` comments are NOT a source (they would double the activity status), and `progress_update` / `system` are pipeline chatter |

Decision dedup keys:
`<scope>:<workspace_id>:<source_ref_id>:<installation_id>:<target_key>` —
workspace + source kind + the ORIGINAL record id + installation + normalized
target. They deliberately EXCLUDE the subscriber list, the route id, the
route revision and any web-client state, so:

- the same persisted team event behind any number of equivalent routes and
  personal subscriptions produces exactly ONE group delivery;
- reading the web inbox, marking read/archived, comment edits, worker
  restarts and repeated scans never re-produce a decision;
- personal DMs stay per-recipient independent (each inbox item is its own
  source record).

Candidate SQL groups equivalent routes by `(source id, installation, target)`
BEFORE applying the page limit. A matching route with current admin authority
and exact active approval wins over non-matching or unauthorized routes; ties
use `(created_at, id)`. Suppression is written only when no equivalent route
matches. Insertion locks and rechecks the selected route's revision, enabled
state and eligibility boundary. Concurrent replicas still produce only one
decision, through the shared unique dedup key.

### Filters are decide-time and recorded

`event_types` selects a subset of each scope's catalog (empty = all); a
cross-scope or unknown event is 400 `route_invalid`. Values are sorted and
deduplicated before storage, so reordering a filter is not a behavior change.
Team `project_id` is evaluated against the issue's project at decision time;
the source records contain no historical project attribution. Non-matching
groups of equivalent rules leave a durable `suppressed/condition_mismatch`
decision.

Changing the installation, normalized target, project or event filter resets
`effective_from`; only source records created after that boundary are eligible.
This also applies when the worker has not yet scanned the old configuration.
Previously persisted decisions keep their content, target and project snapshots.
Re-enabling resets the boundary; repeating `enabled=true` does not. No supported
edit, route recreation or enable cycle replays an already decided source/target.

Disabling and cancelling queued deliveries commit in the same transaction.
The route's `last_disabled_at` additionally invalidates older sending claims,
even if the route is re-enabled before the next shard. An already accepted or
in-flight external request cannot be recalled; its receipt remains auditable.

Personal mute semantics reuse `notification_preference` through the shared
`notify.IsMuted` mapping — the SAME grouping the inbox listeners apply when
creating items. A muted item is decided as `suppressed/recipient_muted`;
a preference that flips AFTER the decision stops the send at the gate. A
preference lookup failure is NEVER read as "not muted": at decision time
the page aborts and the cursor holds; at send time the delivery requeues as
transient.

### Team target approvals (separate scope)

Group/topic team targets require an ACTIVE approval scoped to the exact
**(workspace, source_kind, project_id, installation, target)** — an automation's
approval is never borrowed by a team source, and `activity` and `comment`
approvals do not cover each other. Approval, revocation, send-time checks
and the revoke-cancels-queued-sends transaction mirror the OL-25 contract,
keyed on the delivery's FROZEN target and project range. `project_id=null`
means explicit workspace-wide consent; it is distinct from a project grant.
Changing to another project or to the whole workspace requires a matching
approval. Existing decisions retain their original range even after a route
edit. Real and diagnostic sends carry `source_scope` (`run`, `inbox`,
`activity`, `comment`) separately from `source_kind` (delivery purpose); a
`test_send` uses its own source's consent. `system_notifications` /
OS-level notification settings are unrelated to this pipeline and never act
as a Feishu master switch.

### Compensation

`inbox:new`, `activity:created` and `comment:created` are low-latency
WAKEUPS only (a notify channel; the publisher's goroutine never does DB
work). The guarantee comes from three persistent per-scanner cursors
(`inbox_source_delivery`, `activity_source_delivery`, `comment_source_delivery`)
with fixed cycle bounds, a per-tick row budget and compare-and-set generations.
After the initial historical pass, each source scans only its stable time
watermark minus an overlap window through the cycle's database start time.
The watermark holds behind open transactions, so a source that commits after
the cursor passed is decided in a later window. A failed page never advances
the cursor, and one source class failing never starves the others.
Event bursts coalesce with a one-second minimum interval and a trailing pass.
See [SCANNING.md](SCANNING.md) for transaction bounds, indexes and recovery.
A source cursor includes its last source ID: completed decisions are excluded
in SQL, so all destinations of one source drain even across multiple pages.
The shared loop distinguishes a nonempty page at the same ID from exhaustion.
Suppressed markers are durable decisions, so their retention covers any
rescan window by construction (decision rows are never pruned except by
workspace deletion). Historical backfill beyond a route's `effective_from`
is NOT supported and cannot be reached by disable/enable cycling.

Boundary: `inbox_item` and `activity_log` generation itself depends on
upstream in-memory events — this module guarantees compensable delivery of
PERSISTED sources, not that every business change has a source record.
Issues with no new events stay visible through the existing stuck-issue
sweeper.

### Lifecycle stop paths

The handler mounts both HTTP surfaces through `RegisterLabrastroMessageRoutes`.
Route creation, edits and enable/disable writes share `routes.go`, while
`decisions.go` locks and checks the route scope, revision and eligibility window
before inserting either a run or a personal/team decision. Source-specific
authority and content rules stay at their existing boundaries.

- Route disabled/deleted → queued sends cancelled transactionally. A disabled
  route in any scope, including run, records `last_disabled_at`. The send gate
  cancels decisions whose original `created_at` is at or before that fence,
  including old in-flight claims and later manual retries of `failed` or
  `uncertain` deliveries, even after re-enabling the route.
- Installation revoked → `lifecycle.StopInstallation` disables routes and
  cancels queued sends for ALL scopes (installation-keyed). It records the same
  disable fence; reinstalling the bot and re-enabling a route cannot revive its
  pre-revocation decisions. A manual retry can be queued but is cancelled at
  the send gate with `route_disabled` (`delivery predates the last route disable`).
- **Member removal** → `lifecycle.StopMember` disables the departing
  member's personal routes and cancels their queued private deliveries in
  the same transaction. Team routes authored by them keep their audit trail
  but the continuous-authorization gate cancels their sends.
- **Project deletion** → `lifecycle.StopProject` disables the project-scoped team
  routes, revokes project approvals, and cancels decisions by their frozen
  `source_project_id` inside the delete transaction. Route and approval writes
  take a compatible project lock after external verification; deletion cannot
  leave a newly saved active orphan. A route edited to another project does
  not hide its older decisions from this cleanup.
- Workspace deletion → `lifecycle.SweepWorkspace` removes feedback, approvals,
  receipts, deliveries and routes in that order under the caller's transaction
  and workspace lock (all keyed by `workspace_id`).

## OL-27 HTTP API

All endpoints live under the authenticated workspace-scoped group. Identity
refusals: 403 `message_no_originator` (agent request with no resolvable
human), 403 `message_forbidden`.

| Method & path | purpose | gate |
| --- | --- | --- |
| `GET /api/message-event-catalog` | event/filter catalog for the config UI | member |
| `GET /api/message-routes?source_kind=inbox\|activity\|comment` | list own inbox rules; team rules only for admins | recipient / owner-admin |
| `POST /api/message-routes` | create rule | member (inbox) / owner-admin (team) |
| `PUT /api/message-routes/{routeId}` | update rule, `expected_revision` required | recipient (inbox) / owner-admin (team) |
| `POST /api/message-routes/{routeId}/enable` | enable/disable, `{"enabled":bool,"expected_revision":N}` | recipient (inbox) / owner-admin (team) |
| `DELETE /api/message-routes/{routeId}` | delete rule (queued sends cancelled) | recipient (inbox) / owner-admin (team) |
| `POST /api/message-routes/{routeId}/test-send` | real-path reachability check | recipient (inbox) / owner-admin (team) |
| `GET /api/message-routes/{routeId}/message-deliveries?status=&limit=&offset=` | records page (projection) | recipient (inbox) / owner-admin (team) |
| `GET /api/message-routes/{routeId}/message-deliveries/{deliveryId}` | detail incl. snapshots + receipts | recipient (inbox) / owner-admin (team) |
| `POST /api/message-routes/{routeId}/message-deliveries/{deliveryId}/retry` | re-queue a `failed`/`uncertain` delivery | recipient (inbox) / owner-admin (team) |
| `GET /api/message-approved-targets` | list active team approvals | owner-admin |
| `POST /api/message-approved-targets` | approve one verified external target for a team scope | owner-admin |
| `DELETE /api/message-approved-targets/{targetId}` | revoke; stops queued sends | owner-admin |

### Create personal route — request / response

`POST /api/message-routes` (acting member = the recipient, always):

```json
{
  "source_kind": "inbox",
  "installation_id": "9f1c…",
  "target_type": "member",
  "target_user_id": "0d4b…",
  "event_types": ["issue_assigned", "new_comment"],
  "enabled": true
}
```

`target_user_id` is accepted but IGNORED — the resolved acting member is
the recipient. `event_types` is optional (empty = every type) and validated
against the catalog; an unknown type is 400 `route_invalid`. `201`:

```json
{
  "route": {
    "id": "c1ab…",
    "workspace_id": "bc8c…",
    "autopilot_id": null,
    "source_kind": "inbox",
    "installation_id": "9f1c…",
    "channel_type": "feishu",
    "target_type": "member",
    "target_user_id": "55e0…",
    "target_key": "member:55e0…",
    "project_id": null,
    "event_types": ["issue_assigned", "new_comment"],
    "enabled": true,
    "revision": 1,
    "created_by": "55e0…",
    "updated_by": "55e0…",
    "effective_from": "2026-09-08T02:30:00Z",
    "last_disabled_at": null
  }
}
```

### Create team route — request

`POST /api/message-routes` (workspace owner/admin):

```json
{
  "source_kind": "activity",
  "installation_id": "9f1c…",
  "target_type": "group",
  "target_chat_id": "oc_…",
  "project_id": "aa31…",
  "event_types": ["status_changed"],
  "enabled": true
}
```

`project_id` is optional (null = whole workspace). The target chat must
already hold an ACTIVE approval for this source kind and exact project range. `comment` scope is
the same shape with `"source_kind": "comment"`. There are no
`conditions`/`content_mode` fields on this surface — every source record
inside the window is forwarded or explicitly suppressed.

### Approve a project destination — request / response

`POST /api/message-approved-targets` (workspace owner/admin):

```json
{
  "source_kind": "activity",
  "project_id": "aa31…",
  "installation_id": "9f1c…",
  "target_type": "group",
  "target_chat_id": "oc_…"
}
```

`201` returns `{"approved_target": {...}}`; its fields are `id`, `workspace_id`,
`source_kind`, nullable `project_id`, nullable `autopilot_id` (always null for
team grants), `installation_id`, `target_key`, `target_type`, `approved_by`,
`approved_at`, nullable `revoked_at`. Omit `project_id` (or send null/empty) to
approve the whole workspace explicitly. Approval does not enable a route.

### Response fields for source clients

Route responses use the same stored route shape as OL-25, adding `source_kind`,
nullable UUID `project_id`, string-array `event_types`, and nullable timestamp
`last_disabled_at`. `revision` is an integer; every update/enable request must
send the version it read. `effective_from` is a timestamp, not a client input.

Delivery detail and source-route record projections add nullable string
`source_scope` and nullable UUID `source_project_id`. New source deliveries
always have a known scope; null is reserved for terminal preview test sends
whose deleted route prevented reconstruction. `source_kind="test_send"` does
not imply automation authority. A project team diagnostic record, for example,
contains `{"source_kind":"test_send","source_scope":"activity","source_project_id":"aa31…","autopilot_id":null}`.

The persisted SQL/schema and returned fields are defined by the `labrastro` migrations
`467_labrastro_message_sources` through `473_labrastro_message_project_approval_index`
and `pkg/db/generated/models.go`; source-route projections live in
`pkg/db/generated/labrastro_message.sql.go`. No client-side schema or frontend
files change in this backend stage.

### Event catalog — response

`GET /api/message-event-catalog`:

```json
{
  "personal": {
    "source_kind": "inbox",
    "target_type": "member",
    "event_types": [
      {"type": "status_changed", "group": "status_changes", "label": "Status changed"},
      {"type": "issue_assigned", "group": "assignments", "label": "Issue assigned to you"}
    ]
  },
  "team": [
    {"source_kind": "activity", "events": [
      {"event": "status_changed", "label": "Issue status changed"},
      {"event": "assignee_changed", "label": "Issue assignee changed"}
    ]},
    {"source_kind": "comment", "events": [
      {"event": "comment", "label": "New comment"}
    ]}
  ]
}
```

### Delivery records

The records API mirrors the automation surface exactly (same statuses,
error-code strings, projection listing, retry semantics), keyed by route:

```json
{
  "deliveries": [
    {
      "id": "e452…",
      "workspace_id": "bc8c…",
      "route_id": "c1ab…",
      "route_revision": 1,
      "autopilot_id": null,
      "run_id": null,
      "source_ref_id": "77bd…",
      "source_kind": "activity",
      "source_scope": "activity",
      "source_project_id": "aa31…",
      "status": "sent",
      "shard_total": 1,
      "installation_id": "9f1c…",
      "target_key": "group:oc_…",
      "created_at": "2026-09-08T02:30:01Z"
    }
  ]
}
```

Delivery detail adds `content_snapshot`
(`{"text","summary","source_kind","issue_identifier","issue_title","actor_name","change","assignee_change","body","link"}`),
`target_snapshot` and `receipts` — same shapes as OL-25. `source_ref`
locates the record:

- Activity: `{"source_kind":"activity","activity_id":"…","issue_id":"…","issue_identifier":"MUL-42"}`.
- Comment: `{"source_kind":"comment","comment_id":"…","parent_comment_id":"…","issue_id":"…","issue_identifier":"MUL-42"}`; the parent is omitted for a root comment.
- Inbox: `{"source_kind":"inbox","inbox_item_id":"…","comment_id":"…","issue_id":"…","issue_identifier":"MUL-42"}`; a valid explicit comment UUID in the item's details is retained when present.

References locate feedback targets; they grant no permission.

For `assignee_changed`, `content_snapshot.assignee_change` freezes the source
record's typed identities independently of display names:

```json
{
  "change": "changed assignee: Member Sam → Agent Sam",
  "assignee_change": {
    "from_type": "member",
    "from_id": "55e0…",
    "to_type": "agent",
    "to_id": "8dc1…"
  }
}
```

An unassigned side has both type and ID set to `""` and renders as `Unassigned`,
so removing a member assignment reads `Member Sam → Unassigned`. If a name
cannot be resolved within the source workspace, the message shows the type
and original ID instead. The rendered text and typed identities are frozen
together; renaming/removing an assignee or retrying delivery never rewrites
them. Other event kinds omit `assignee_change`. Existing frozen decisions
remain unchanged and are not backfilled or resent.

### New error codes (stable strings)

| code | HTTP | when |
| --- | --- | --- |
| `route_not_self` | 403 | managing a personal route you are not the recipient of, or an inbox payload naming another user |
| `message_no_originator` | 403 | agent request with no resolvable human originator |
| `message_forbidden` | 403 | agent request whose originator is not a workspace member |
| `message_target_admin_required` | 403 | team configuration/approval by a member below admin |

Delivery `error_code` additions recorded by the pipeline:
`recipient_muted` (suppressed at decision or cancelled at the send gate).

### Upgrade and rollback

Apply all pending migrations before running this binary, including the scanner
migrations `9010_labrastro_message_delivery_run_index` through
`9015_labrastro_message_source_watermark` (see [SCANNING.md](SCANNING.md)).
The personal/team source upgrade and rollback notes below cover the `labrastro`
stems from `467_labrastro_message_sources` through `473_labrastro_message_project_approval_index`.
Stop delivery workers
while upgrading a populated preview deployment (the `labrastro` migrations from `467_labrastro_message_sources`
through `470_labrastro_message_approved_target_source_active_index`): old rows
cannot prove their historical project consent or last-disable boundary.
`471_labrastro_message_source_scope`
withdraws preview team approvals, disables team routes, cancels all unfinished
non-run deliveries (including diagnostics), and clears their leases. It keeps
sent/suppressed/cancelled history, receipts and dedup identities. Personal
routes retain their enabled flag but start a new eligibility window; automation
configuration, approvals and deliveries are unchanged. Re-approve each desired
team project/workspace range, then explicitly enable its route. Cancelled
preview work is not automatically replayed.

The project approval index in `473_labrastro_message_project_approval_index` uses
`NULLS NOT DISTINCT`, keeping one active grant for each exact range, including the workspace range. Each concurrent
index build has its own migration and registered invalid-index retry cleanup.
The migration regression deliberately fails the build with duplicates, then
repairs and retries through the real runner.

Downgrading `473_labrastro_message_project_approval_index` refuses BEFORE any DDL
if an active project grant exists: revoke those grants first. After that
precondition, `472_labrastro_message_drop_preview_approval_index` can restore the
old workspace index and `471_labrastro_message_source_scope` withdraws remaining
source activity before dropping scope fields.
Downgrading `467_labrastro_message_sources` deletes all non-run deliveries
INCLUDING test sends with null
`autopilot_id`, their receipts, source routes and approvals. Export that audit
history before a downgrade if it must be retained. Legacy automation records
and receipts remain. The populated migration test covers upgrade, rejected
downgrade, successful retry, receipt cleanup and re-upgrade.

## Known gaps (explicitly out of this stage)

- Real-Feishu verification (member/group/topic, service-side dedup window)
  requires an authorized test bot; the contract above is covered by DB-backed
  tests with fake senders/verifiers plus HTTP-contract tests on the real
  client.
- The frontend configuration UI remains a later stage of the parent plan.
- Verified `source_ref` and receipt/report context now feed the designated
  agent conversation; they do not directly insert a member-authored comment.
