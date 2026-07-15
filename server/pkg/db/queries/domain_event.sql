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

-- DeleteDispatchedDomainEventsBefore is the retention sweep (MUL-4332 §4.1):
-- it only reclaims events already marked 'dispatched' and older than the TTL
-- cutoff, in bounded batches so a large backlog never monopolizes the DB. In
-- PR1 nothing dispatches events, so this is a no-op until the PR3 matcher runs.
-- name: DeleteDispatchedDomainEventsBefore :execrows
DELETE FROM domain_event
WHERE id IN (
    SELECT de.id FROM domain_event de
    WHERE de.dispatch_status = 'dispatched'
      AND de.created_at < $1
    ORDER BY de.seq
    LIMIT $2
);
