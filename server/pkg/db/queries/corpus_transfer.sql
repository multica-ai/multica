-- name: CreateOrGetCorpusTransfer :one
INSERT INTO corpus_transfer (
    id,
    workspace_id,
    actor_id,
    idempotency_key,
    object_key,
    manifest,
    manifest_sha256,
    expected_size_bytes,
    expected_sha256,
    expires_at
) VALUES (
    sqlc.arg('id'),
    sqlc.arg('workspace_id'),
    sqlc.arg('actor_id'),
    sqlc.arg('idempotency_key'),
    sqlc.arg('object_key'),
    sqlc.arg('manifest'),
    sqlc.arg('manifest_sha256'),
    sqlc.arg('expected_size_bytes'),
    sqlc.arg('expected_sha256'),
    sqlc.arg('expires_at')
)
ON CONFLICT (workspace_id, actor_id, idempotency_key) DO UPDATE
SET idempotency_key = EXCLUDED.idempotency_key
RETURNING *;

-- name: GetCorpusTransfer :one
SELECT *
FROM corpus_transfer
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = sqlc.arg('id');

-- name: ClaimCorpusTransferUpload :one
UPDATE corpus_transfer
SET state = 'uploading',
    upload_started_at = now(),
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = sqlc.arg('id')
  AND state = 'created'
  AND expires_at > now()
RETURNING *;

-- name: MarkCorpusTransferUploaded :one
UPDATE corpus_transfer
SET state = 'uploaded',
    uploaded_at = now(),
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = sqlc.arg('id')
  AND state = 'uploading'
RETURNING *;

-- name: FailCorpusTransferUpload :one
UPDATE corpus_transfer
SET state = 'failed',
    failure_code = sqlc.arg('failure_code'),
    failed_at = now(),
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = sqlc.arg('id')
  AND state = 'uploading'
RETURNING *;

-- name: ClaimCorpusTransferVerification :one
UPDATE corpus_transfer
SET state = 'verifying',
    verification_token = sqlc.arg('verification_token'),
    verification_lease_expires_at = sqlc.arg('verification_lease_expires_at'),
    verification_started_at = now(),
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = sqlc.arg('id')
  AND (
      state = 'uploaded'
      OR (state = 'verifying' AND verification_lease_expires_at <= now())
  )
RETURNING *;

-- name: ConfirmCorpusTransfer :one
UPDATE corpus_transfer
SET state = 'confirmed',
    verification_token = NULL,
    verification_lease_expires_at = NULL,
    verified_size_bytes = sqlc.arg('verified_size_bytes'),
    verified_sha256 = sqlc.arg('verified_sha256'),
    confirmed_at = now(),
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = sqlc.arg('id')
  AND state = 'verifying'
  AND verification_token = sqlc.arg('verification_token')
  AND expected_size_bytes = sqlc.arg('verified_size_bytes')
  AND expected_sha256 = sqlc.arg('verified_sha256')
RETURNING *;

-- name: FailCorpusTransferVerification :one
UPDATE corpus_transfer
SET state = 'failed',
    verification_token = NULL,
    verification_lease_expires_at = NULL,
    failure_code = sqlc.arg('failure_code'),
    failed_at = now(),
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = sqlc.arg('id')
  AND state = 'verifying'
  AND verification_token = sqlc.arg('verification_token')
RETURNING *;

-- name: ExpireCorpusTransfer :one
UPDATE corpus_transfer
SET state = 'expired',
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = sqlc.arg('id')
  AND state IN ('created', 'uploading')
  AND expires_at <= now()
RETURNING *;

-- name: GetConfirmedCorpusTransferContent :one
SELECT *
FROM corpus_transfer
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = sqlc.arg('id')
  AND state IN ('confirmed', 'acked');

-- name: CreateCorpusTransferACK :one
INSERT INTO corpus_transfer_ack (
    workspace_id,
    transfer_id,
    sink_id,
    confirmed_sha256,
    acknowledged_by
) VALUES (
    sqlc.arg('workspace_id'),
    sqlc.arg('transfer_id'),
    sqlc.arg('sink_id'),
    sqlc.arg('confirmed_sha256'),
    sqlc.arg('acknowledged_by')
)
ON CONFLICT (workspace_id, transfer_id, sink_id) DO NOTHING
RETURNING *;

-- name: GetCorpusTransferACK :one
SELECT *
FROM corpus_transfer_ack
WHERE workspace_id = sqlc.arg('workspace_id')
  AND transfer_id = sqlc.arg('transfer_id')
  AND sink_id = sqlc.arg('sink_id');

-- name: MarkCorpusTransferAcked :one
UPDATE corpus_transfer
SET state = 'acked',
    updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = sqlc.arg('id')
  AND state IN ('confirmed', 'acked')
  AND verified_sha256 = sqlc.arg('confirmed_sha256')
RETURNING *;
