ALTER TABLE "user"
  ADD COLUMN onboarding_questionnaire JSONB NOT NULL DEFAULT '{}'::jsonb;
