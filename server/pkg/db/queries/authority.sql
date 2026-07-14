-- name: DeleteExpiredAuthorityNonces :exec
DELETE FROM authority_nonce
WHERE expires_at <= now();

-- name: ClaimAuthorityNonce :execrows
INSERT INTO authority_nonce (nonce_hash, expires_at)
VALUES ($1, now() + sqlc.arg('ttl')::interval)
ON CONFLICT (nonce_hash) DO NOTHING;
