-- =====================
-- Labrastro message delivery (OL-25)
-- =====================
-- SQL is the only source for this module's reads and writes; the generated
-- code under pkg/db/generated is rebuilt with `make sqlc` and never edited.

-- =====================
-- labrastro_message_route
-- =====================

-- name: CreateLabrastroMessageRoute :one
-- All scopes use the same write; personal/team routes pass NULL autopilot_id.
INSERT INTO labrastro_message_route (
    id, workspace_id, autopilot_id, installation_id, channel_type,
    target_type, target_user_id, target_chat_id, target_message_id,
    target_thread_id, target_key, conditions, content_mode,
    enabled, revision, created_by, updated_by, effective_from, source_kind, project_id, event_types
) VALUES (
    $1, $2, $3, $4, $5,
    $6, sqlc.narg('target_user_id'), sqlc.narg('target_chat_id'),
    sqlc.narg('target_message_id'), sqlc.narg('target_thread_id'),
    $7, $8, $9,
    $10, 1, $11, $11, now(), sqlc.arg('source_kind'), sqlc.narg('project_id'), sqlc.arg('event_types')
) RETURNING *;

-- name: GetLabrastroMessageRoute :one
-- Workspace-scoped: a route id from another workspace is not found.
SELECT * FROM labrastro_message_route
WHERE id = $1 AND workspace_id = $2;

-- name: ListLabrastroMessageRoutesByAutopilot :many
SELECT * FROM labrastro_message_route
WHERE workspace_id = $1 AND autopilot_id = $2
ORDER BY created_at, id;

-- name: UpdateLabrastroMessageRoute :one
-- Revision-guarded edit: the WHERE clause carries the revision the caller
-- read, so a stale save conflicts (zero rows) instead of silently
-- overwriting a newer edit. Config edits never rewrite deliveries already
-- decided — those hold frozen snapshots.
UPDATE labrastro_message_route
SET installation_id = sqlc.arg('installation_id'),
    channel_type = sqlc.arg('channel_type'),
    target_type = sqlc.arg('target_type'),
    target_user_id = sqlc.narg('target_user_id'),
    target_chat_id = sqlc.narg('target_chat_id'),
    target_message_id = sqlc.narg('target_message_id'),
    target_thread_id = sqlc.narg('target_thread_id'),
    target_key = sqlc.arg('target_key'),
    conditions = sqlc.arg('conditions'),
    content_mode = sqlc.arg('content_mode'),
    project_id = sqlc.narg('project_id'),
    event_types = sqlc.arg('event_types'),
    enabled = sqlc.arg('enabled'),
    last_disabled_at = CASE WHEN NOT sqlc.arg('enabled')::boolean AND enabled
        THEN clock_timestamp() ELSE last_disabled_at END,
    revision = revision + 1,
    updated_by = sqlc.arg('updated_by'),
    updated_at = now(),
    -- Enabling through an edit restarts the eligibility boundary with the
    -- same semantics as an explicit enable: no backfill of the disabled
    -- window. Run edits retain their boundary; personal/team destination and
    -- filter changes start a new window, preserving their existing semantics.
    effective_from = CASE
        WHEN (sqlc.arg('enabled')::boolean AND NOT enabled)
          OR (source_kind <> 'run' AND (installation_id IS DISTINCT FROM sqlc.arg('installation_id')
            OR target_key IS DISTINCT FROM sqlc.arg('target_key')
            OR project_id IS DISTINCT FROM sqlc.narg('project_id')
            OR event_types IS DISTINCT FROM sqlc.arg('event_types')))
        THEN clock_timestamp()
        ELSE effective_from
    END
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND source_kind = sqlc.arg('source_kind')
  AND revision = sqlc.arg('expected_revision')
RETURNING *;

-- name: SetLabrastroMessageRouteEnabled :one
-- Enable/disable with revision bump. Disable keeps history; the queued
-- deliveries are cancelled by the service layer. Only a disabled-to-enabled
-- transition resets effective_from so the disabled window is never backfilled.
UPDATE labrastro_message_route
SET enabled = sqlc.arg('enabled'),
    last_disabled_at = CASE WHEN NOT sqlc.arg('enabled')::boolean AND enabled
        THEN clock_timestamp() ELSE last_disabled_at END,
    revision = revision + 1,
    updated_by = sqlc.arg('updated_by'),
    updated_at = now(),
    effective_from = CASE
        WHEN sqlc.arg('enabled')::boolean AND NOT enabled THEN clock_timestamp()
        ELSE effective_from
    END
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND source_kind = sqlc.arg('source_kind')
  AND revision = sqlc.arg('expected_revision')
RETURNING *;

-- name: DeleteLabrastroMessageRoute :exec
DELETE FROM labrastro_message_route
WHERE id = $1 AND workspace_id = $2;

-- =====================
-- labrastro_message_delivery
-- =====================

-- name: CreateLabrastroMessageDelivery :many
-- The decision insert. ON CONFLICT DO NOTHING against
-- uq_labrastro_message_delivery_dedup is the exactly-once guard: two
-- replicas deciding the same (source, target) race safely, the loser sees
-- zero rows. Empty result means a decision already exists.
INSERT INTO labrastro_message_delivery (
    id, workspace_id, route_id, route_revision, autopilot_id, run_id,
    dedup_key, source_kind, status, content_snapshot, target_snapshot,
    shard_total, source_ref, target_key, installation_id, error_code,
    lease_token, lease_expires_at, requested_by, source_scope, source_project_id, source_ref_id
) VALUES (
    $1, $2, sqlc.narg('route_id'), sqlc.narg('route_revision'), $3,
    sqlc.narg('run_id'), $4, $5, $6, $7, $8, $9, sqlc.narg('source_ref'),
    $10, $11, sqlc.narg('error_code'),
    sqlc.narg('lease_token'), sqlc.narg('lease_expires_at'), sqlc.narg('requested_by'),
    COALESCE(sqlc.narg('source_scope')::text, 'run'), sqlc.narg('source_project_id'), sqlc.narg('source_ref_id')
)
ON CONFLICT (dedup_key) DO NOTHING
RETURNING *;

-- name: GetLabrastroMessageDelivery :one
SELECT * FROM labrastro_message_delivery
WHERE id = $1 AND workspace_id = $2;

-- name: ClaimDueLabrastroMessageDelivery :one
-- Claims one due delivery. SKIP LOCKED distributes the queue across
-- workers and replicas; the lease token makes a crashed claim visible to
-- the expiry sweep. The lease is a scheduling optimization, not the
-- exactly-once guard — the fixed per-shard send UUID and the receipt
-- ledger carry that.
WITH candidate AS (
    SELECT id
    FROM labrastro_message_delivery
    WHERE status = 'queued'
      AND next_attempt_at <= now()
      AND (lease_expires_at IS NULL OR lease_expires_at <= now())
    ORDER BY next_attempt_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE labrastro_message_delivery AS d
SET status = 'sending',
    lease_token = gen_random_uuid(),
    lease_expires_at = now() + interval '2 minutes',
    first_attempt_at = COALESCE(first_attempt_at, now()),
    updated_at = now()
FROM candidate
WHERE d.id = candidate.id
RETURNING d.*;

-- name: RetryClaimedLabrastroMessageDelivery :one
-- Transient send failure: back off and requeue under the same lease guard.
UPDATE labrastro_message_delivery
SET status = 'queued',
    next_attempt_at = sqlc.arg('next_attempt_at'),
    attempts = attempts + 1,
    error_code = sqlc.narg('error_code'),
    last_error = sqlc.narg('last_error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND lease_token = sqlc.arg('lease_token')
  AND status = 'sending'
  AND lease_expires_at > clock_timestamp()
RETURNING *;

-- name: CompleteClaimedLabrastroMessageDelivery :one
-- Every shard accepted with receipts recorded.
UPDATE labrastro_message_delivery
SET status = 'sent',
    delivered_at = now(),
    attempts = attempts + 1,
    error_code = NULL,
    last_error = NULL,
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND lease_token = sqlc.arg('lease_token')
  AND status = 'sending'
  AND lease_expires_at > clock_timestamp()
RETURNING *;

-- name: FailClaimedLabrastroMessageDelivery :one
-- Definitive failure under the lease guard: bad target, revoked
-- installation, exhausted attempts. error_code is stable and
-- machine-readable; last_error carries the human explanation.
UPDATE labrastro_message_delivery
SET status = 'failed',
    attempts = attempts + 1,
    error_code = sqlc.narg('error_code'),
    last_error = sqlc.narg('last_error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND lease_token = sqlc.arg('lease_token')
  AND status = 'sending'
  AND lease_expires_at > clock_timestamp()
RETURNING *;

-- name: CancelClaimedLabrastroMessageDelivery :one
-- Pre-send gate refused (route disabled/deleted, source archived/missing)
-- while the claim was held. Recorded as a decision, not an error.
UPDATE labrastro_message_delivery
SET status = 'cancelled',
    error_code = sqlc.narg('error_code'),
    last_error = sqlc.narg('last_error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND lease_token = sqlc.arg('lease_token')
  AND status = 'sending'
  AND lease_expires_at > clock_timestamp()
RETURNING *;

-- name: UncertainClaimedLabrastroMessageDelivery :one
-- Same transition without the attempts increment, for a worker that
-- classified an ambiguous HTTP outcome while still holding its lease.
UPDATE labrastro_message_delivery
SET status = 'uncertain',
    error_code = sqlc.narg('error_code'),
    last_error = sqlc.narg('last_error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND lease_token = sqlc.arg('lease_token')
  AND status = 'sending'
  AND lease_expires_at > clock_timestamp()
RETURNING *;

-- name: RequeueExpiredLabrastroMessageDeliveryClaims :many
-- Crash recovery for claims whose worker died mid-send. The lease lapsed
-- while the row was 'sending', so the HTTP call may or may not have left
-- this process — the row goes to 'uncertain', not 'queued'. Bounded by
-- the same unique send UUID as manual retries.
UPDATE labrastro_message_delivery
SET status = 'uncertain',
    error_code = sqlc.narg('error_code'),
    last_error = sqlc.narg('last_error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE status = 'sending'
  AND (lease_expires_at IS NULL OR lease_expires_at <= now())
RETURNING *;

-- name: SetLabrastroMessageDeliveryOutcome :one
-- Terminal write for a SYNCHRONOUS send. Same ownership contract as every
-- other result write (repair contract §5, review R13): ID + lease token +
-- 'sending' must all match, so an old request whose lease expired and was
-- re-claimed by someone else can never overwrite the new owner's state.
UPDATE labrastro_message_delivery
SET status = sqlc.arg('status'),
    delivered_at = CASE
        WHEN sqlc.arg('status')::text = 'sent' THEN now()
        ELSE delivered_at
    END,
    attempts = attempts + 1,
    error_code = sqlc.narg('error_code'),
    last_error = sqlc.narg('last_error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND lease_token = sqlc.arg('lease_token')
  AND status = 'sending'
  AND lease_expires_at > clock_timestamp()
RETURNING *;

-- name: ClaimLabrastroMessageDeliveryByID :one
-- Claims ONE known row for a synchronous sender (test-send): created queued
-- and claimed inside the same parent-lock transaction, so the caller owns a
-- bounded lease without ever touching the shared queue scan.
UPDATE labrastro_message_delivery
SET status = 'sending',
    lease_token = gen_random_uuid(),
    lease_expires_at = now() + interval '2 minutes',
    first_attempt_at = COALESCE(first_attempt_at, now()),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND status = 'queued'
RETURNING *;

-- name: RetryLabrastroMessageDeliveryManually :one
-- Operator verify-and-retry. Allowed from 'failed' (cause fixed) and
-- 'uncertain' (operator checked; the fixed send UUID keeps the retry
-- idempotent inside the platform's dedup window). A zero-row result is a
-- status that is not retryable, or a lost race with a worker.
UPDATE labrastro_message_delivery
SET status = 'queued',
    next_attempt_at = now(),
    error_code = sqlc.narg('error_code'),
    last_error = sqlc.narg('last_error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND status IN ('failed', 'uncertain')
RETURNING *;

-- name: CancelLabrastroMessageDeliveriesByRoute :many
-- Route disabled or deleted: cancel sends that have not started. Sends the
-- platform already received cannot be recalled; their records stay.
UPDATE labrastro_message_delivery
SET status = 'cancelled',
    error_code = sqlc.narg('error_code'),
    last_error = sqlc.narg('last_error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE route_id = sqlc.arg('route_id')
  AND status = 'queued'
RETURNING *;

-- name: CancelLabrastroMessageDeliveriesByInstallation :many
-- The bot was disconnected/revoked: stop everything this installation had
-- not started yet. The worker's own installation re-check covers the race
-- where a claim lands between revoke and this sweep.
UPDATE labrastro_message_delivery
SET status = 'cancelled',
    error_code = sqlc.narg('error_code'),
    last_error = sqlc.narg('last_error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE installation_id = sqlc.arg('installation_id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND status = 'queued'
RETURNING *;

-- name: ListLabrastroMessageDeliveriesByAutopilot :many
-- Records API projection. content_snapshot / target_snapshot / source_ref
-- are deliberately excluded — a page of deliveries must not carry full
-- message bodies or target snapshots just to render a status list.
SELECT
    d.id, d.workspace_id, d.route_id, d.route_revision, d.autopilot_id,
    d.run_id, d.dedup_key, d.source_kind, d.status, d.attempts,
    d.next_attempt_at, d.error_code, d.last_error, d.shard_total,
    d.delivered_at, d.first_attempt_at, d.created_at, d.updated_at,
    d.installation_id, d.target_key, d.requested_by
FROM labrastro_message_delivery d
WHERE d.workspace_id = sqlc.arg('workspace_id')
  AND d.autopilot_id = sqlc.arg('autopilot_id')
  AND (sqlc.narg('run_id')::uuid IS NULL OR d.run_id = sqlc.narg('run_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR d.status = sqlc.narg('status')::text)
ORDER BY d.created_at DESC, d.id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- =====================
-- labrastro_message_receipt
-- =====================

-- name: ClaimLabrastroMessageReceipt :one
-- Creates the receipt row for one shard on FIRST attempt, or returns the
-- existing row. The send_uuid is written once and never changes, so a
-- retry replays the identical idempotency key. DO UPDATE (not DO NOTHING)
-- so the fixed row comes back either way.
INSERT INTO labrastro_message_receipt (
    delivery_id, workspace_id, installation_id,
    shard_index, shard_total, send_uuid
) VALUES (
    sqlc.arg('delivery_id'), sqlc.arg('workspace_id'),
    sqlc.arg('installation_id'), sqlc.arg('shard_index'),
    sqlc.arg('shard_total'), sqlc.arg('send_uuid')
)
ON CONFLICT (delivery_id, shard_index) DO UPDATE SET
    updated_at = now()
RETURNING *;

-- name: RecordLabrastroMessageReceiptExternalID :one
-- First external id wins. A duplicate-accepted retry response must not
-- overwrite the id the platform first reported.
UPDATE labrastro_message_receipt
SET external_message_id = sqlc.arg('external_message_id'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND external_message_id IS NULL
RETURNING *;

-- name: ListLabrastroMessageReceiptsByDelivery :many
SELECT * FROM labrastro_message_receipt
WHERE delivery_id = sqlc.arg('delivery_id')
  AND workspace_id = sqlc.arg('workspace_id')
ORDER BY shard_index;

-- =====================
-- Parent-integrity locks (workspace teardown race, R3)
-- =====================

-- name: LockWorkspaceForMessageDecision :one
-- Share-locks the workspace row a new delivery/receipt write depends on.
-- The DeleteWorkspace flow first takes the same row FOR UPDATE, so either
-- the delete commits first (this row is gone; the caller must refuse the
-- write) or this lock is granted first (the delete then sweeps the new
-- rows by workspace_id). Either order, no orphan containing message
-- content can outlive the workspace.
SELECT id FROM workspace
WHERE id = $1
FOR SHARE;

-- name: GetLabrastroMessageDeliveryLease :one
-- Fresh lease ownership read. A worker validates this before starting each
-- NEW shard send: once its claim is lost (lease expired, another owner,
-- state moved on), it must stop dialing even though its in-memory snapshot
-- still looks claimed.
SELECT lease_token, lease_expires_at, status,
    COALESCE(lease_expires_at > clock_timestamp(), false)::boolean AS lease_active
FROM labrastro_message_delivery
WHERE id = $1;

-- =====================
-- Compensation scan
-- =====================

-- name: ListLabrastroMessageDeliveryCandidateRoutes :many
-- (run, route) pairs where the run reached a terminal state inside the
-- route's eligibility window but the normalized target still has NO
-- delivery decision. Suppressed/cancelled decisions count — a source the
-- module has already judged is never re-decided. Full-missing-set scan by
-- design: a monotonic cursor would skip runs that committed late, so the
-- NOT EXISTS shrinking set IS the cursor. Optional run_id narrows the same
-- candidate path for a direct enqueue without changing compensation semantics.
SELECT sqlc.embed(r), sqlc.embed(a), sqlc.embed(rt),
    COALESCE(r.task_id, CASE WHEN r.issue_id IS NULL THEN (
        SELECT t.id FROM agent_task_queue t
        WHERE t.autopilot_run_id = r.id ORDER BY t.created_at LIMIT 1
    ) END)::uuid AS source_task_id
FROM autopilot_run r
JOIN autopilot a ON a.id = r.autopilot_id
JOIN labrastro_message_route rt
  ON rt.autopilot_id = r.autopilot_id
 AND rt.workspace_id = a.workspace_id
 AND rt.enabled = true
WHERE r.status IN ('completed', 'failed', 'skipped')
  AND (sqlc.narg('run_id')::uuid IS NULL OR r.id = sqlc.narg('run_id')::uuid)
  AND a.status <> 'archived'
  AND r.completed_at >= rt.effective_from
  AND NOT EXISTS (
      SELECT 1 FROM labrastro_message_delivery d
      WHERE d.run_id = r.id
        AND d.installation_id = rt.installation_id
        AND d.target_key = rt.target_key
  )
ORDER BY r.completed_at, r.id, rt.id
LIMIT sqlc.arg('limit');

-- name: ListStaleRunOnlyAutopilotTasks :many
-- Tasks whose run_only automation has not reached a terminal run state even
-- though the task itself is terminal — the event the run sync listens for
-- was lost. The scanner feeds these to the EXISTING SyncRunFromTask logic;
-- this module runs no state machine of its own.
-- Either direction of the task/run link is persisted evidence of run_only
-- execution (repair contract §3, review R7), when there is no issue link. The
-- autopilot's current execution_mode is mutable and must never filter
-- historical sources.
SELECT t.* FROM agent_task_queue t
JOIN autopilot_run r ON r.id = t.autopilot_run_id
    OR (t.autopilot_run_id IS NULL AND r.task_id = t.id)
WHERE r.issue_id IS NULL
  AND r.status IN ('pending', 'issue_created', 'running')
  AND t.status IN ('completed', 'failed', 'cancelled')
  AND t.id > sqlc.arg('after_id')::uuid
  AND t.id <= sqlc.arg('upper_id')::uuid
ORDER BY t.id
LIMIT sqlc.arg('limit');

-- name: ListStaleCreateIssueAutopilotIssues :many
-- Issues whose create_issue automation run never saw the terminal
-- transition. No status filter here on purpose: custom statuses inherit
-- canonical terminal states through Effective() inside SyncRunFromIssue,
-- and this query cannot see that mapping. The non-terminal run join keeps
-- the candidate set small; a non-terminal issue is a harmless no-op sync.
-- Same principle on the issue side: the run's own issue link identifies
-- create_issue sources.
SELECT i.* FROM issue i
JOIN autopilot_run r ON r.issue_id = i.id
WHERE r.issue_id IS NOT NULL
  AND r.status IN ('pending', 'issue_created', 'running')
  AND i.id > sqlc.arg('after_id')::uuid
  AND i.id <= sqlc.arg('upper_id')::uuid
ORDER BY i.id
LIMIT sqlc.arg('limit');

-- =====================
-- Member binding (installation-precise)
-- =====================

-- name: GetChannelUserBindingForDelivery :one
-- Resolves a member target through ONE pinned installation. Deliberately
-- NOT FindChannelBindingForMember: that query picks the most recent
-- binding per channel_type, which picks the wrong bot when a workspace runs
-- several installations of the same channel (OL-24 §4.2). The join re-checks
-- the installation row is still active so a revoked bot never resolves.
SELECT b.* FROM channel_user_binding b
JOIN channel_installation ci ON ci.id = b.installation_id
JOIN member m ON m.workspace_id = b.workspace_id AND m.user_id = b.multica_user_id
WHERE b.workspace_id = sqlc.arg('workspace_id')
  AND b.installation_id = sqlc.arg('installation_id')
  AND b.multica_user_id = sqlc.arg('multica_user_id')
  AND ci.workspace_id = b.workspace_id
  AND b.channel_type = ci.channel_type
  AND ci.status = 'active';

-- =====================
-- Workspace teardown
-- =====================

-- name: DeleteLabrastroMessageReceiptsByWorkspace :exec
DELETE FROM labrastro_message_receipt
WHERE workspace_id = $1;

-- name: DeleteLabrastroMessageDeliveriesByWorkspace :exec
DELETE FROM labrastro_message_delivery
WHERE workspace_id = $1;

-- name: DeleteLabrastroMessageRoutesByWorkspace :exec
DELETE FROM labrastro_message_route
WHERE workspace_id = $1;

-- =====================
-- Approved external targets (repair contract §2, review R1)
-- =====================

-- name: ApproveLabrastroMessageTarget :one
-- Owner/admin approval of ONE (automation, bot, normalized target) triple.
-- Re-approving a previously revoked target inserts a fresh active row; the
-- revoked history stays queryable. The unique partial index
-- uq_labrastro_message_approved_target_active makes a concurrent double
-- approval a 23505, which the handler reports as already-approved.
INSERT INTO labrastro_message_approved_target (
    workspace_id, autopilot_id, installation_id, target_key, target_type, approved_by
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('autopilot_id'), sqlc.arg('installation_id'),
    sqlc.arg('target_key'), sqlc.arg('target_type'), sqlc.arg('approved_by')
)
RETURNING *;

-- name: RevokeLabrastroMessageTarget :many
-- Revocation is soft: the row remains as audit history and the active check
-- (revoked_at IS NULL) stops satisfying immediately. Queued deliveries are
-- cancelled by the service layer on this path.
UPDATE labrastro_message_approved_target
SET revoked_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND autopilot_id = sqlc.arg('autopilot_id')
  AND installation_id = sqlc.arg('installation_id')
  AND target_key = sqlc.arg('target_key')
  AND revoked_at IS NULL
RETURNING *;

-- name: GetActiveLabrastroMessageApprovedTarget :one
-- The single authorization lookup used by save/enable/test-send/worker/retry:
-- is this exact (source, bot, target) triple currently approved?
SELECT * FROM labrastro_message_approved_target
WHERE workspace_id = sqlc.arg('workspace_id')
  AND autopilot_id = sqlc.arg('autopilot_id')
  AND installation_id = sqlc.arg('installation_id')
  AND target_key = sqlc.arg('target_key')
  AND revoked_at IS NULL
FOR SHARE;

-- name: ListLabrastroMessageApprovedTargets :many
SELECT * FROM labrastro_message_approved_target
WHERE workspace_id = sqlc.arg('workspace_id')
  AND autopilot_id = sqlc.arg('autopilot_id')
  AND revoked_at IS NULL
ORDER BY approved_at DESC, id;

-- name: DeleteLabrastroMessageApprovedTargetsByAutopilot :exec
DELETE FROM labrastro_message_approved_target
WHERE workspace_id = $1 AND autopilot_id = $2;

-- name: DeleteLabrastroMessageApprovedTargetsByInstallation :exec
DELETE FROM labrastro_message_approved_target
WHERE workspace_id = $1 AND installation_id = $2;

-- name: DeleteLabrastroMessageApprovedTargetsByWorkspace :exec
DELETE FROM labrastro_message_approved_target
WHERE workspace_id = $1;

-- =====================
-- Compensation scan cursors (repair contract §4, review R4)
-- =====================

-- name: GetLabrastroMessageScanCursor :one
SELECT * FROM labrastro_message_scan_cursor
WHERE scanner = $1;

-- name: SaveLabrastroMessageScanCursor :one
-- Compare-and-set on the generation: a replica resuming from an older cycle
-- loses the race (zero rows) and must reload before writing. Callers that
-- finish a cycle bump the generation and reset the position in the same
-- statement.
UPDATE labrastro_message_scan_cursor
SET cursor_ts = sqlc.arg('cursor_ts'),
    cursor_id = sqlc.arg('cursor_id'),
    cycle_started_at = sqlc.arg('cycle_started_at'),
    cycle_upper_id = sqlc.narg('cycle_upper_id'),
    cycle_stable_at = sqlc.narg('cycle_stable_at'),
    generation = generation + 1,
    updated_at = now()
WHERE scanner = sqlc.arg('scanner')
  AND generation = sqlc.arg('expected_generation')
RETURNING *;

-- name: InitLabrastroMessageScanCursor :one
INSERT INTO labrastro_message_scan_cursor (scanner)
VALUES (sqlc.arg('scanner'))
ON CONFLICT (scanner) DO NOTHING
RETURNING *;

-- name: GetLabrastroMessageSourceScanHorizon :one
-- Source created_at defaults to transaction start, not commit time. There is
-- no enforced maximum transaction duration, so never advance past a writer
-- still in flight. An unobservable/disabled backend or enabled two-phase
-- commit has no trustworthy start bound: retain history. Prepared transactions
-- can disappear from activity before becoming visible in pg_prepared_xacts.
-- Userless maintenance workers (including autovacuum) cannot insert sources;
-- their hidden state must not pin an ordinary application's watermark.
SELECT statement_timestamp()::timestamptz AS scan_through,
    LEAST(statement_timestamp(),
        COALESCE((SELECT min(CASE WHEN state IS NULL OR state = 'disabled'
            THEN 'epoch'::timestamptz ELSE xact_start END)
            FROM pg_catalog.pg_stat_activity
            WHERE datname = current_database() AND usesysid IS NOT NULL), statement_timestamp()),
        CASE WHEN current_setting('max_prepared_transactions')::integer > 0
            THEN 'epoch'::timestamptz ELSE statement_timestamp() END
    )::timestamptz AS stable_at;

-- name: ListStaleLinkedIssueTaskFailures :many
-- create_issue tasks reach the run through the ISSUE link (their own
-- autopilot_run_id is NULL), and SyncRunFromLinkedIssueTask is the existing
-- state machine that settles the run from its own dispatch chain. The
-- historical query name is retained, but both completed and failed tasks
-- compensate lost terminal events while the run is active. The syncer checks
-- dispatch ancestry and the latest attempt; unrelated mention tasks cannot
-- settle the run. Tasks without an issue never enter this scan.
SELECT t.* FROM agent_task_queue t
JOIN issue i ON i.id = t.issue_id
JOIN autopilot_run r ON r.issue_id = i.id
WHERE t.autopilot_run_id IS NULL
  AND t.issue_id IS NOT NULL
  AND t.status IN ('completed', 'failed')
  AND r.status IN ('pending', 'issue_created', 'running')
  AND t.id > sqlc.arg('after_id')::uuid
  AND t.id <= sqlc.arg('upper_id')::uuid
ORDER BY t.id
LIMIT sqlc.arg('limit');

-- name: CancelLabrastroMessageDeliveriesByTarget :many
-- An approval revocation stops the route's not-yet-started sends against
-- the FROZEN target (delivery rows pin installation + target_key at
-- decision time, so this matches on those copies, never the live route).
UPDATE labrastro_message_delivery
SET status = 'cancelled',
    error_code = sqlc.narg('error_code'),
    last_error = sqlc.narg('last_error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND autopilot_id = sqlc.arg('autopilot_id')
  AND installation_id = sqlc.arg('installation_id')
  AND target_key = sqlc.arg('target_key')
  AND status = 'queued'
RETURNING *;

-- name: GetLabrastroMessageScanUpperBound :one
-- Freeze an immutable ID bound over the source table, including non-candidates.
-- Old IDs that become eligible later are revisited in the next full cycle.
SELECT COALESCE(CASE WHEN sqlc.arg('scan_issues')::boolean THEN
    (SELECT id FROM issue ORDER BY id DESC LIMIT 1)
ELSE
    (SELECT id FROM agent_task_queue ORDER BY id DESC LIMIT 1)
END, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS upper_id;

-- name: LockLabrastroMessageInstallation :one
SELECT * FROM channel_installation
WHERE id = $1 AND workspace_id = $2
FOR SHARE;

-- name: CancelLabrastroMessageDeliveriesByAutopilot :exec
UPDATE labrastro_message_delivery
SET status = 'cancelled', error_code = 'source_archived',
    last_error = 'source automation archived', lease_token = NULL,
    lease_expires_at = NULL, updated_at = now()
WHERE autopilot_id = $1 AND workspace_id = $2 AND status = 'queued';

-- name: DisableLabrastroMessageRoutesByInstallation :exec
UPDATE labrastro_message_route
SET enabled = false,
    last_disabled_at = clock_timestamp(),
    revision = revision + 1, updated_at = now()
WHERE workspace_id = $1 AND installation_id = $2 AND enabled;

-- name: LockLabrastroMessageRuntimeInstallations :many
SELECT ci.* FROM channel_installation ci
JOIN agent a ON a.id = ci.agent_id
WHERE a.runtime_id = $1 AND a.kind = 'system'
ORDER BY ci.id FOR UPDATE OF ci;

-- =====================
-- OL-27: personal-inbox and team-event sources
-- =====================
-- The same route/delivery/receipt storage the automation source uses; these
-- queries add the source-scoped configuration, the candidate scans over the
-- three persisted source records, the team target approvals and the
-- lifecycle stop paths. Shared delivery/cleanup queries above also serve runs.

-- name: ListLabrastroMessageSourceRoutes :many
-- Resolve current membership and visibility together; personal config is self-only.
SELECT r.* FROM labrastro_message_route r
JOIN member viewer ON viewer.workspace_id = r.workspace_id
    AND viewer.user_id = sqlc.arg('user_id')::uuid
WHERE r.workspace_id = sqlc.arg('workspace_id')
  AND ((r.source_kind = 'inbox' AND r.target_user_id = viewer.user_id)
       OR (r.source_kind IN ('activity', 'comment') AND viewer.role IN ('owner', 'admin')))
  AND (sqlc.narg('source_kind')::text IS NULL OR r.source_kind = sqlc.narg('source_kind')::text)
ORDER BY r.created_at, r.id;

-- ---- candidate scans ----
-- Collapse equivalent routes BEFORE pagination. A matching, authorized rule
-- wins over every non-match; attribution is deterministic within that set.
-- The inclusive cursor drains all targets of the last source across pages.
-- NOT EXISTS removes completed targets, so no target cursor is required.
-- Each cycle scans only [stable watermark - overlap, cycle start]. The
-- next overlapping cycle recovers sources that committed behind the cursor.

-- name: ListLabrastroMessageInboxSourceCandidates :many
SELECT DISTINCT ON (i.id, rt.installation_id, rt.target_key)
    i.id AS item_id, i.workspace_id AS item_workspace_id,
    i.recipient_id, i.type AS item_type, i.severity AS item_severity,
    i.title AS item_title, i.body AS item_body, i.details AS item_details,
    i.created_at AS item_created_at, i.issue_id AS item_issue_id,
    iss.number AS issue_number, iss.title AS issue_title,
    w.slug AS workspace_slug, w.issue_prefix AS workspace_issue_prefix,
    rt.id AS route_id, rt.revision AS route_revision,
    rt.installation_id, rt.channel_type,
    rt.target_type, rt.target_user_id, rt.target_chat_id,
    rt.target_message_id, rt.target_thread_id, rt.target_key,
    rt.event_types, rt.effective_from, rt.created_at AS route_created_at
FROM inbox_item i
JOIN labrastro_message_route rt
  ON rt.workspace_id = i.workspace_id
 AND rt.source_kind = 'inbox'
 AND rt.enabled = true
 AND rt.target_type = 'member'
 AND rt.target_user_id = i.recipient_id
JOIN workspace w ON w.id = i.workspace_id
LEFT JOIN issue iss ON iss.id = i.issue_id
WHERE i.recipient_type = 'member'
  AND i.created_at >= rt.effective_from
  AND i.created_at >= sqlc.arg('scan_from')::timestamptz
  AND i.created_at <= sqlc.arg('scan_through')::timestamptz
  AND i.id >= sqlc.arg('after_id')::uuid
  AND i.id <= sqlc.arg('upper_id')::uuid
  AND NOT EXISTS (
      SELECT 1 FROM labrastro_message_delivery d
      WHERE d.workspace_id = i.workspace_id
        AND d.source_scope = rt.source_kind
        AND d.source_ref_id = i.id
        AND d.installation_id = rt.installation_id
        AND d.target_key = rt.target_key
  )
ORDER BY i.id, rt.installation_id, rt.target_key,
    (cardinality(rt.event_types) = 0 OR i.type = ANY(rt.event_types)) DESC NULLS LAST, rt.created_at, rt.id
LIMIT sqlc.arg('limit');

-- name: ListLabrastroMessageActivitySourceCandidates :many
SELECT DISTINCT ON (al.id, rt.installation_id, rt.target_key)
    al.id AS activity_id, al.workspace_id AS activity_workspace_id,
    al.issue_id AS activity_issue_id,
    al.actor_type AS activity_actor_type, al.actor_id AS activity_actor_id,
    al.action AS activity_action, al.details AS activity_details,
    al.created_at AS activity_created_at,
    iss.number AS issue_number, iss.title AS issue_title,
    iss.project_id AS issue_project_id, iss.status AS issue_status,
    w.slug AS workspace_slug, w.issue_prefix AS workspace_issue_prefix,
    au.name AS actor_member_name, ag.name AS actor_agent_name,
    rt.id AS route_id, rt.revision AS route_revision,
    rt.installation_id, rt.channel_type,
    rt.target_type, rt.target_user_id, rt.target_chat_id,
    rt.target_message_id, rt.target_thread_id, rt.target_key,
    rt.project_id AS route_project_id, rt.event_types, rt.effective_from,
    rt.created_at AS route_created_at, rt.updated_by AS route_updated_by
FROM activity_log al
JOIN issue iss ON iss.id = al.issue_id
JOIN workspace w ON w.id = al.workspace_id
LEFT JOIN "user" au ON al.actor_type = 'member' AND au.id = al.actor_id
LEFT JOIN agent ag ON al.actor_type = 'agent' AND ag.id = al.actor_id
JOIN labrastro_message_route rt
  ON rt.workspace_id = al.workspace_id
 AND rt.source_kind = 'activity'
 AND rt.enabled = true
LEFT JOIN member route_authorizer
  ON route_authorizer.workspace_id = rt.workspace_id AND route_authorizer.user_id = rt.updated_by
LEFT JOIN labrastro_message_approved_target approval
  ON approval.workspace_id = rt.workspace_id AND approval.source_kind = rt.source_kind
 AND approval.installation_id = rt.installation_id AND approval.target_key = rt.target_key
 AND approval.project_id IS NOT DISTINCT FROM rt.project_id AND approval.revoked_at IS NULL
WHERE al.action IN ('status_changed', 'assignee_changed')
  AND al.created_at >= rt.effective_from
  AND al.created_at >= sqlc.arg('scan_from')::timestamptz
  AND al.created_at <= sqlc.arg('scan_through')::timestamptz
  AND al.id >= sqlc.arg('after_id')::uuid
  AND al.id <= sqlc.arg('upper_id')::uuid
  AND NOT EXISTS (
      SELECT 1 FROM labrastro_message_delivery d
      WHERE d.workspace_id = al.workspace_id
        AND d.source_scope = rt.source_kind
        AND d.source_ref_id = al.id
        AND d.installation_id = rt.installation_id
        AND d.target_key = rt.target_key
  )
ORDER BY al.id, rt.installation_id, rt.target_key,
    (((rt.project_id IS NULL OR rt.project_id = iss.project_id) AND (cardinality(rt.event_types) = 0 OR al.action = ANY(rt.event_types))) AND route_authorizer.role IN ('owner', 'admin') AND approval.id IS NOT NULL) DESC NULLS LAST,
    ((rt.project_id IS NULL OR rt.project_id = iss.project_id) AND (cardinality(rt.event_types) = 0 OR al.action = ANY(rt.event_types))) DESC NULLS LAST, rt.created_at, rt.id
LIMIT sqlc.arg('limit');

-- name: ListLabrastroMessageCommentSourceCandidates :many
-- Only plain comments enter the source (the type whitelist is definitional
-- SQL, not a service filter): status_change comments would double the
-- activity status source, and progress/system entries are pipeline chatter.
SELECT DISTINCT ON (c.id, rt.installation_id, rt.target_key)
    c.id AS comment_id, c.workspace_id AS comment_workspace_id,
    c.issue_id AS comment_issue_id, c.parent_id AS comment_parent_id,
    c.author_type AS comment_author_type, c.author_id AS comment_author_id,
    c.content AS comment_content, c.created_at AS comment_created_at,
    iss.number AS issue_number, iss.title AS issue_title,
    iss.project_id AS issue_project_id, iss.status AS issue_status,
    w.slug AS workspace_slug, w.issue_prefix AS workspace_issue_prefix,
    au.name AS author_member_name, ag.name AS author_agent_name,
    rt.id AS route_id, rt.revision AS route_revision,
    rt.installation_id, rt.channel_type,
    rt.target_type, rt.target_user_id, rt.target_chat_id,
    rt.target_message_id, rt.target_thread_id, rt.target_key,
    rt.project_id AS route_project_id, rt.event_types, rt.effective_from,
    rt.created_at AS route_created_at, rt.updated_by AS route_updated_by
FROM comment c
JOIN issue iss ON iss.id = c.issue_id
JOIN workspace w ON w.id = c.workspace_id
LEFT JOIN "user" au ON c.author_type = 'member' AND au.id = c.author_id
LEFT JOIN agent ag ON c.author_type = 'agent' AND ag.id = c.author_id
JOIN labrastro_message_route rt
  ON rt.workspace_id = c.workspace_id
 AND rt.source_kind = 'comment'
 AND rt.enabled = true
LEFT JOIN member route_authorizer
  ON route_authorizer.workspace_id = rt.workspace_id AND route_authorizer.user_id = rt.updated_by
LEFT JOIN labrastro_message_approved_target approval
  ON approval.workspace_id = rt.workspace_id AND approval.source_kind = rt.source_kind
 AND approval.installation_id = rt.installation_id AND approval.target_key = rt.target_key
 AND approval.project_id IS NOT DISTINCT FROM rt.project_id AND approval.revoked_at IS NULL
WHERE c.type = 'comment'
  AND c.deleted_at IS NULL
  AND c.created_at >= rt.effective_from
  AND c.created_at >= sqlc.arg('scan_from')::timestamptz
  AND c.created_at <= sqlc.arg('scan_through')::timestamptz
  AND c.id >= sqlc.arg('after_id')::uuid
  AND c.id <= sqlc.arg('upper_id')::uuid
  AND NOT EXISTS (
      SELECT 1 FROM labrastro_message_delivery d
      WHERE d.workspace_id = c.workspace_id
        AND d.source_scope = rt.source_kind
        AND d.source_ref_id = c.id
        AND d.installation_id = rt.installation_id
        AND d.target_key = rt.target_key
  )
ORDER BY c.id, rt.installation_id, rt.target_key,
    (((rt.project_id IS NULL OR rt.project_id = iss.project_id) AND (cardinality(rt.event_types) = 0 OR c.type = ANY(rt.event_types))) AND route_authorizer.role IN ('owner', 'admin') AND approval.id IS NOT NULL) DESC NULLS LAST,
    ((rt.project_id IS NULL OR rt.project_id = iss.project_id) AND (cardinality(rt.event_types) = 0 OR c.type = ANY(rt.event_types))) DESC NULLS LAST, rt.created_at, rt.id
LIMIT sqlc.arg('limit');

-- name: GetLabrastroMessageSourceScanUpperBound :one
-- Freezes one cycle's immutable ID bound over the source table, including
-- non-candidates. Late commits are revisited within the next time window;
-- IDs are never treated as a timestamp (the tables contain UUIDv4 and v7).
SELECT COALESCE(CASE sqlc.arg('source_kind')::text
    WHEN 'inbox' THEN (SELECT id FROM inbox_item ORDER BY id DESC LIMIT 1)
    WHEN 'activity' THEN (SELECT id FROM activity_log ORDER BY id DESC LIMIT 1)
    WHEN 'comment' THEN (SELECT id FROM comment ORDER BY id DESC LIMIT 1)
END, '00000000-0000-0000-0000-000000000000'::uuid)::uuid AS upper_id;

-- ---- delivery records for source routes ----

-- name: ListLabrastroMessageDeliveriesByRoute :many
-- Records API projection for a personal/team route — same shape and
-- exclusions as the automation listing (no message bodies in a page).
SELECT
    d.id, d.workspace_id, d.route_id, d.route_revision, d.autopilot_id,
    d.run_id, d.source_ref_id, d.dedup_key, d.source_kind, d.status,
    d.attempts, d.next_attempt_at, d.error_code, d.last_error,
    d.shard_total, d.delivered_at, d.first_attempt_at, d.created_at,
    d.updated_at, d.installation_id, d.target_key, d.source_scope, d.source_project_id
FROM labrastro_message_delivery d
WHERE d.workspace_id = sqlc.arg('workspace_id')
  AND d.route_id = sqlc.arg('route_id')
  AND (sqlc.narg('status')::text IS NULL OR d.status = sqlc.narg('status')::text)
ORDER BY d.created_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- ---- team target approvals (workspace-admin consent, OL-27 scope) ----

-- name: ApproveLabrastroMessageSourceTarget :one
-- Active approval for one (workspace, source kind, project, bot, target) scope. A
-- concurrent duplicate insert is a 23505 against
-- uq_labrastro_message_approved_target_project_active, reported as
-- already-approved.
INSERT INTO labrastro_message_approved_target (
    workspace_id, autopilot_id, installation_id, target_key, target_type,
    source_kind, approved_by, project_id
) VALUES (
    sqlc.arg('workspace_id'), NULL, sqlc.arg('installation_id'),
    sqlc.arg('target_key'), sqlc.arg('target_type'),
    sqlc.arg('source_kind'), sqlc.arg('approved_by'), sqlc.narg('project_id')
)
RETURNING *;

-- name: GetActiveLabrastroMessageSourceApprovedTarget :one
-- The single authorization lookup for team sends: is this exact
-- (source kind, project, bot, target) range currently approved? An automation's
-- approval never satisfies it and vice versa.
SELECT * FROM labrastro_message_approved_target
WHERE workspace_id = sqlc.arg('workspace_id')
  AND source_kind = sqlc.arg('source_kind')
  AND autopilot_id IS NULL
  AND installation_id = sqlc.arg('installation_id')
  AND target_key = sqlc.arg('target_key')
  AND project_id IS NOT DISTINCT FROM sqlc.narg('project_id')
  AND revoked_at IS NULL
FOR SHARE;

-- name: ListLabrastroMessageSourceApprovedTargets :many
SELECT * FROM labrastro_message_approved_target
WHERE workspace_id = sqlc.arg('workspace_id')
  AND source_kind IN ('activity', 'comment')
  AND revoked_at IS NULL
ORDER BY approved_at DESC, id;

-- name: RevokeLabrastroMessageSourceTarget :many
-- Soft revoke with the same audit semantics as the automation approvals.
UPDATE labrastro_message_approved_target
SET revoked_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND source_kind = sqlc.arg('source_kind')
  AND autopilot_id IS NULL
  AND installation_id = sqlc.arg('installation_id')
  AND target_key = sqlc.arg('target_key')
  AND project_id IS NOT DISTINCT FROM sqlc.narg('project_id')
  AND revoked_at IS NULL
RETURNING *;

-- name: CancelLabrastroMessageDeliveriesBySourceTarget :many
-- An approval revocation stops the not-yet-started team sends against the
-- FROZEN scope, project and target, including diagnostic sends. The live
-- route never supplies the historical authorization range.
UPDATE labrastro_message_delivery
SET status = 'cancelled',
    error_code = sqlc.narg('error_code'),
    last_error = sqlc.narg('last_error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND source_scope = sqlc.arg('source_kind')
  AND source_project_id IS NOT DISTINCT FROM sqlc.narg('project_id')
  AND installation_id = sqlc.arg('installation_id')
  AND target_key = sqlc.arg('target_key')
  AND status = 'queued'
RETURNING *;

-- ---- lifecycle stop paths ----

-- name: RevokeLabrastroMessageSourceTargetsByProject :exec
-- Keep historical consent, but a deleted project must leave no active grant.
UPDATE labrastro_message_approved_target
SET revoked_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND project_id = sqlc.arg('project_id')
  AND revoked_at IS NULL;

-- name: DisableLabrastroMessageSourceRoutesByProject :many
-- Project deletion stops the team routes scoped to it (the filter can
-- never match again). Runs inside the project-delete transaction; the
-- service layer cancels their queued sends in the same transaction.
UPDATE labrastro_message_route
SET enabled = false, last_disabled_at = clock_timestamp(), revision = revision + 1, updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND project_id = sqlc.arg('project_id')
  AND source_kind IN ('activity', 'comment')
  AND enabled
RETURNING id;

-- name: CancelLabrastroMessageDeliveriesByProject :many
UPDATE labrastro_message_delivery d
SET status = 'cancelled',
    error_code = sqlc.narg('error_code'),
    last_error = sqlc.narg('last_error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE d.workspace_id = sqlc.arg('workspace_id')
  AND d.source_project_id = sqlc.arg('project_id')
  AND status = 'queued'
RETURNING id;

-- name: DisableLabrastroMessagePersonalRoutesByUser :many
-- Member removal stops the member's personal forwarding rules — the
-- recipient can no longer hold a binding, and the rules are meaningless
-- until re-invite + explicit re-enable.
UPDATE labrastro_message_route
SET enabled = false, last_disabled_at = clock_timestamp(), revision = revision + 1, updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND source_kind = 'inbox'
  AND target_user_id = sqlc.arg('target_user_id')
  AND enabled
RETURNING id;

-- name: CancelLabrastroMessageDeliveriesByPersonalRecipient :many
UPDATE labrastro_message_delivery
SET status = 'cancelled',
    error_code = sqlc.narg('error_code'),
    last_error = sqlc.narg('last_error'),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND source_scope = 'inbox'
  AND target_key = sqlc.arg('target_key')
  AND status = 'queued'
RETURNING id;

-- name: LockLabrastroMessageRoute :one
-- SHARE conflicts with configuration edits/deletion until a decision commits.
SELECT * FROM labrastro_message_route
WHERE id = $1 AND workspace_id = $2
FOR SHARE;

-- name: LockLabrastroMessageSourceProject :one
-- The same parent lock used by project deletion, before locking any route.
SELECT id FROM project
WHERE id = $1 AND workspace_id = $2
FOR KEY SHARE;
