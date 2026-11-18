-- Set per-transaction attribution consumed by trg_issue_archive_on_delete.
-- A direct SQL delete intentionally leaves these unset, so the trigger records
-- actor_type='unknown' and the database session/network context instead.
-- name: SetIssueAuditContext :exec
SELECT
    set_config(
        'app.audit_actor_id',
        coalesce(sqlc.narg('actor_id')::uuid::text, ''),
        true
    ),
    set_config(
        'app.audit_actor_type',
        sqlc.arg('actor_type')::text,
        true
    ),
    set_config(
        'app.audit_client_fingerprint',
        coalesce(sqlc.narg('client_fingerprint')::text, ''),
        true
    ),
    set_config(
        'app.audit_source_ip',
        coalesce(sqlc.narg('source_ip')::inet::text, ''),
        true
    );
