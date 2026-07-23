-- FIR-3754: service tokens must always expire. Existing non-expiring tokens
-- are revoked and expired before the column becomes mandatory, so this
-- migration never preserves an unbounded access path.
WITH revoked_nonexpiring AS (
    UPDATE cerebro_service_token
    SET revoked = TRUE,
        expires_at = now()
    WHERE expires_at IS NULL
      AND revoked = FALSE
    RETURNING id, workspace_id
)
INSERT INTO cerebro_service_token_audit (
    service_token_id, workspace_id, event, actor_user_id, detail
)
SELECT id, workspace_id, 'revoked', NULL,
       '{"reason":"migration_expiry_required","migration":"9157"}'::jsonb
FROM revoked_nonexpiring;

-- Already-revoked legacy rows need a bounded expiry too, but no new
-- revocation event occurred and therefore no second revocation audit is due.
UPDATE cerebro_service_token
SET expires_at = now()
WHERE expires_at IS NULL;

-- Remove every legacy mutation grant. A token left with no readable area is
-- revoked, while still remaining visible to admins. The scope mutation and
-- its durable audit event are one statement, so they cannot diverge.
WITH hardened_scopes AS (
    UPDATE cerebro_service_token
    SET scopes = scopes - 'skills:write' - 'agents:write' - 'issues:write'
    WHERE scopes ?| ARRAY['skills:write', 'agents:write', 'issues:write']
    RETURNING id, workspace_id, scopes
)
INSERT INTO cerebro_service_token_audit (
    service_token_id, workspace_id, event, actor_user_id, detail
)
SELECT id, workspace_id, 'scope_hardened', NULL,
       jsonb_build_object(
           'reason', 'migration_write_scopes_removed',
           'migration', '9157',
           'scopes', scopes
       )
FROM hardened_scopes;

WITH revoked_empty AS (
    UPDATE cerebro_service_token
    SET revoked = TRUE
    WHERE scopes = '[]'::jsonb
      AND revoked = FALSE
    RETURNING id, workspace_id
)
INSERT INTO cerebro_service_token_audit (
    service_token_id, workspace_id, event, actor_user_id, detail
)
SELECT id, workspace_id, 'revoked', NULL,
       '{"reason":"migration_no_read_scopes","migration":"9157"}'::jsonb
FROM revoked_empty;

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
