-- Transactional-outbox domain event log (MUL-4332 §4.1). CreateDomainEvent is
-- the only write; callers invoke it on a *db.Queries already bound to their
-- own pgx.Tx (WithTx) so the event commits atomically with the domain fact.

-- name: CreateDomainEvent :one
-- dispatch_status ('pending'), available_at (now()), attempts (0), seq
-- (nextval) and created_at (now()) all come from column defaults — this is a
-- root outbox write, never a re-queue, so the writer never sets them.
INSERT INTO domain_event (
    id,
    workspace_id,
    type,
    schema_version,
    subject_type,
    subject_id,
    actor_type,
    actor_id,
    payload,
    correlation_id,
    causation_execution_id,
    causation_action_index,
    hop_count
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
RETURNING *;

-- name: GetDomainEvent :one
SELECT * FROM domain_event
WHERE id = $1;

-- name: ListDomainEventsByCorrelation :many
-- Bounded correlation-chain read for the debug API. The LIMIT is pushed into the
-- query (not applied after loading the whole chain) and rides the
-- (workspace_id, correlation_id, seq) index (MUL-4332 PR3 review round: correlation).
SELECT * FROM domain_event
WHERE workspace_id = $1
  AND correlation_id = $2
ORDER BY seq ASC
LIMIT $3;

-- name: CountDomainEventsBySubject :one
SELECT count(*) FROM domain_event
WHERE subject_type = $1
  AND subject_id = $2;

-- NOTE: the retention/TTL delete is intentionally NOT defined in PR1. The
-- correct predicate is "dispatched AND older than TTL AND every related
-- hook_execution is terminal" (MUL-4332 §4.1/§9), and hook_execution does not
-- exist until PR3. Shipping a weaker "dispatched + TTL" delete now would risk
-- reclaiming still-executing audit sources the moment PR3 enables dispatching
-- (review point 5). The query lands in PR3 with the full terminal predicate.

-- name: ClaimOneEventWithCandidates :many
-- The durable matcher's claim AND revision pin, in a SINGLE statement (MUL-4332 PR3
-- review round: matcher point 1). It leases exactly one undispatched, now-available
-- event in seq order and, in the same statement, materializes that event's complete
-- (hook_id, revision_id) candidate set with each revision's configuration.
--
-- One statement is what makes the pin real. PostgreSQL's default READ COMMITTED
-- gives every statement its own snapshot, so claiming in one statement and then
-- selecting candidates in another — even inside one transaction — lets a revision
-- edit committed in between change this event's decision, and lets two candidates be
-- decided against revisions from different instants. Here the claim and the whole
-- candidate set share one snapshot, so the pinned revisions are exactly those active
-- at claim time (§5.1 "使用 matcher claim 时的当前 enabled revision").
--
-- The LEFT JOIN LATERAL means an event with no candidates still returns exactly one
-- row (hook_id NULL), so the caller can distinguish "claimed, nothing matched" from
-- "nothing to claim" (zero rows). Reclaims events left 'dispatching' by a crashed
-- matcher once their lease expires, so processing is at-least-once. FOR UPDATE SKIP
-- LOCKED lets multiple matchers share the queue and keeps the claimed row locked for
-- the rest of the transaction. Rides idx_domain_event_dispatch.
--
-- Issue scope is lifecycle ownership only and does NOT restrict the event subject —
-- that is the job of `when` — so scope is not a filter here.
WITH claimed AS (
    UPDATE domain_event
    SET dispatch_status = 'dispatching',
        lease_token = @lease_token,
        lease_expires_at = @lease_expires_at,
        attempts = attempts + 1
    WHERE id = (
        SELECT id FROM domain_event
        WHERE available_at <= now()
          AND (dispatch_status = 'pending'
               OR (dispatch_status = 'dispatching' AND lease_expires_at < now()))
        ORDER BY seq ASC
        LIMIT 1
        FOR UPDATE SKIP LOCKED
    )
    RETURNING id, workspace_id, type, subject_id, actor_type, actor_id,
              payload, correlation_id, hop_count
)
SELECT
    c.id             AS event_id,
    c.workspace_id   AS event_workspace_id,
    c.type           AS event_type,
    c.subject_id     AS event_subject_id,
    c.actor_type     AS event_actor_type,
    c.actor_id       AS event_actor_id,
    c.payload        AS event_payload,
    c.correlation_id AS event_correlation_id,
    c.hop_count      AS event_hop_count,
    cand.hook_id     AS hook_id,
    cand.revision_id AS revision_id,
    cand.match       AS match,
    cand.conditions  AS conditions,
    -- COALESCE so the no-candidate row (every cand.* NULL) still scans into a
    -- non-nullable string; callers gate on hook_id being present.
    COALESCE(cand.fire_mode, '')::text AS fire_mode
FROM claimed c
LEFT JOIN LATERAL (
    SELECT h.id AS hook_id, h.created_at AS hook_created_at,
           r.id AS revision_id, r.match, r.conditions, r.fire_mode
    FROM hook h
    JOIN hook_revision r ON r.id = h.active_revision_id
    WHERE h.workspace_id = c.workspace_id
      AND h.enabled = true
      AND h.archived_at IS NULL
      AND r.event_type = c.type
) cand ON true
ORDER BY cand.hook_created_at ASC NULLS LAST, cand.hook_id ASC;

-- name: DeferDomainEventDispatch :execrows
-- Back off one event after a transient dispatch failure. The failed decision rolled
-- back (including its claim), so without this the event would sit at the head of the
-- queue and be re-claimed immediately, spinning on the same failure and starving
-- everything behind it.
UPDATE domain_event
SET available_at = now() + make_interval(secs => @backoff_seconds::int),
    attempts = attempts + 1
WHERE id = $1;

-- name: GetOwnedDomainEventForDispatch :one
-- Row-lock one claimed event and assert lease OWNERSHIP before the matcher writes
-- any decision (MUL-4332 PR3 review round: matcher point 2). The predicate is
-- deliberately identical to the one MarkDomainEventDispatched/Failed use, and both
-- evaluate the expiry against DATABASE clock time (clock_timestamp(), not now(),
-- which is frozen at transaction start): a worker holding the right token whose
-- lease has nonetheless expired is NOT the owner and must write nothing. Returning
-- no rows is the fail-closed signal.
SELECT * FROM domain_event
WHERE id = $1
  AND dispatch_status = 'dispatching'
  AND lease_token = $2
  AND lease_expires_at > clock_timestamp()
FOR UPDATE;

-- name: MarkDomainEventDispatched :execrows
-- Finalize a matched event under the SAME ownership predicate as the entry
-- assertion, so a lease that expired mid-decision cannot commit the decision. The
-- caller MUST assert this returns exactly 1 — a 0 means ownership was lost and the
-- whole transaction (every execution and latch it wrote) must roll back.
-- dispatched_at is set here (and only here) so the retention/audit boundary can
-- rely on "dispatched ⇒ dispatched_at".
UPDATE domain_event
SET dispatch_status = 'dispatched',
    dispatched_at = now(),
    lease_token = NULL,
    lease_expires_at = NULL
WHERE id = $1
  AND dispatch_status = 'dispatching'
  AND lease_token = $2
  AND lease_expires_at > clock_timestamp();

-- name: MarkDomainEventFailed :execrows
-- Terminal failure for an event the matcher can never decode (a malformed payload
-- fails identically on every retry). Recording it 'failed' instead of leaving it
-- pending stops one poison event from being re-leased forever. Same ownership
-- predicate as the dispatched path.
UPDATE domain_event
SET dispatch_status = 'failed',
    dispatched_at = now(),
    lease_token = NULL,
    lease_expires_at = NULL
WHERE id = $1
  AND dispatch_status = 'dispatching'
  AND lease_token = $2
  AND lease_expires_at > clock_timestamp();
