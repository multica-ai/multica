ALTER TABLE "user" ADD COLUMN onboarded_at TIMESTAMPTZ;

-- Grandfather existing users. Treat any row that already exists at the
-- moment this column lands as already-onboarded so the deploy doesn't
-- wall them off behind a flow they never asked for. `created_at` (not
-- NOW()) keeps analytics honest — "signup → onboarded interval" reads
-- as 0 for pre-launch users, which under grandfathering semantics is
-- accurate ("they onboarded implicitly at signup").
UPDATE "user" SET onboarded_at = created_at;
