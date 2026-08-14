CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_user_oidc_identity_issuer_subject ON user_oidc_identity (issuer, subject);
