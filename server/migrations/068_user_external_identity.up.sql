-- Add OIDC / external IDP identity binding to user table.
--
-- Allows a multica user to be linked to a stable identifier from any
-- external OIDC IDP (Authentik / Keycloak / Okta / Auth0). The pair
-- (external_provider, external_user_id) is the lookup key on every
-- subsequent login; email becomes a backfill / display field rather
-- than a stable identifier.
--
-- Backwards compatible: existing rows have NULL external_user_id, which
-- the partial unique index excludes. Email-only and Google-OAuth users
-- continue to authenticate unchanged. First-time OIDC login of an
-- existing email user backfills the external identity on the existing
-- row (handled in handler/auth_oidc.go findOrLinkUserByOIDC).

ALTER TABLE "user" ADD COLUMN external_user_id  TEXT;
ALTER TABLE "user" ADD COLUMN external_provider TEXT;

-- Partial unique index: enforce one row per (provider, external_id) when
-- external_id is set. NULL rows (legacy / non-OIDC) are not covered, so
-- the existing email-key uniqueness still governs them.
CREATE UNIQUE INDEX user_external_identity_key
  ON "user" (external_provider, external_user_id)
  WHERE external_user_id IS NOT NULL;
