-- An earlier iteration of migration 051 added `onboarding_current_step`
-- and then later the same PR removed it (the column was never actually
-- consumed by the shipping code). Any environment that pulled the
-- interim revision and ran `migrate-up` has a dead column; any
-- environment that only sees the final 051 never will. This migration
-- collapses both into the same final schema. IF EXISTS makes it a
-- no-op on fresh environments.
ALTER TABLE "user" DROP COLUMN IF EXISTS onboarding_current_step;
