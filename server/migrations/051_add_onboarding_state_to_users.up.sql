ALTER TABLE "user"
  ADD COLUMN IF NOT EXISTS onboarding_questionnaire JSONB NOT NULL DEFAULT '{}'::jsonb;
