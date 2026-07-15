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
SELECT * FROM domain_event
WHERE workspace_id = $1
  AND correlation_id = $2
ORDER BY seq ASC;

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
