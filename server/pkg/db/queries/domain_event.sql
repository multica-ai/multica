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

-- name: ClaimPendingDomainEvents :many
-- The durable matcher's claim scan (MUL-4332 PR3): lease a bounded batch of
-- undispatched, now-available events in seq order. Reclaims events left
-- 'dispatching' by a crashed matcher once their lease expires, so processing is
-- at-least-once. FOR UPDATE SKIP LOCKED lets multiple matchers share the queue;
-- the LIMIT bounds the lock hold. Rides idx_domain_event_dispatch.
UPDATE domain_event
SET dispatch_status = 'dispatching',
    lease_token = @lease_token,
    lease_expires_at = @lease_expires_at,
    attempts = attempts + 1
WHERE id IN (
    SELECT id FROM domain_event
    WHERE available_at <= now()
      AND (dispatch_status = 'pending'
           OR (dispatch_status = 'dispatching' AND lease_expires_at < now()))
    ORDER BY seq ASC
    LIMIT @max_events::int
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: GetDomainEventForDispatch :one
-- Row-lock one claimed event so the matcher can re-assert lease ownership BEFORE
-- it writes any decision (MUL-4332 PR3 review round: matcher point 2). The lock is
-- held for the rest of the authoritative transaction, so a concurrent
-- ClaimPendingDomainEvents cannot steal the lease mid-decision — it blocks here and
-- then re-checks dispatch_status. A stale holder whose lease WAS already reclaimed
-- sees a different lease_token and must abort without writing anything.
SELECT * FROM domain_event
WHERE id = $1
FOR UPDATE;

-- name: MarkDomainEventDispatched :execrows
-- Finalize a matched event. CAS on lease_token so only the current lease holder
-- can advance it (a stale/expired matcher whose lease was reclaimed cannot). The
-- caller MUST assert this returns exactly 1 — a 0 means the lease was lost and the
-- decision must not be counted as dispatched. dispatched_at is set here (and only
-- here) so the retention/audit boundary can rely on "dispatched ⇒ dispatched_at".
UPDATE domain_event
SET dispatch_status = 'dispatched',
    dispatched_at = now(),
    lease_token = NULL,
    lease_expires_at = NULL
WHERE id = $1 AND lease_token = $2;

-- name: MarkDomainEventFailed :execrows
-- Terminal failure for an event the matcher can never decode (a malformed payload
-- fails identically on every retry). Recording it 'failed' instead of leaving it
-- pending stops one poison event from being re-leased forever. Same lease CAS as
-- the dispatched path.
UPDATE domain_event
SET dispatch_status = 'failed',
    dispatched_at = now(),
    lease_token = NULL,
    lease_expires_at = NULL
WHERE id = $1 AND lease_token = $2;
