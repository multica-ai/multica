-- FIR-3754: service tokens must always expire. Existing non-expiring tokens
-- are revoked and expired before the column becomes mandatory, so this
-- migration never preserves an unbounded access path.
UPDATE cerebro_service_token
SET revoked = TRUE,
    expires_at = now()
WHERE expires_at IS NULL;

-- Remove every legacy mutation grant. A token left with no readable area is
-- revoked, while still remaining visible to admins and in the audit history.
UPDATE cerebro_service_token
SET scopes = scopes - 'skills:write' - 'agents:write' - 'issues:write';

UPDATE cerebro_service_token
SET revoked = TRUE
WHERE scopes = '[]'::jsonb;

ALTER TABLE cerebro_service_token
    ALTER COLUMN expires_at SET NOT NULL,
    ADD CONSTRAINT cerebro_service_token_expiry_after_creation
        CHECK (expires_at > created_at),
    ADD CONSTRAINT cerebro_service_token_read_only_scopes
        CHECK (
            revoked OR (
                jsonb_array_length(scopes) > 0
                AND scopes <@ '["skills:read","agents:read","issues:read"]'::jsonb
            )
        );
