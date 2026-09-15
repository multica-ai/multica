-- Keep each concurrent index build in its own migration statement.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS external_auth_identity_provider_subject_uidx
    ON external_auth_identity (provider, issuer, subject);
