-- Also provides the user_id lookup path without relying on a foreign key.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS external_auth_identity_user_provider_uidx
    ON external_auth_identity (user_id, provider, issuer);
