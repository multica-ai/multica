-- Reverse 068: drop external identity binding.
-- Data loss: any external_user_id values are discarded. After rollback,
-- previously OIDC-bound users fall back to email-link path on next login.

DROP INDEX IF EXISTS user_external_identity_key;
ALTER TABLE "user" DROP COLUMN IF EXISTS external_provider;
ALTER TABLE "user" DROP COLUMN IF EXISTS external_user_id;
