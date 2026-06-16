-- v3 reverts the v2 attempt at persisting Step 3 runtime choice on the user
-- row. The two columns were added in an earlier draft of this migration
-- (wip/onboarding-v2, never shipped to production) and the design moved to
-- a frontend Zustand transient store instead — onboarding state collapses
-- back to the single `onboarded_at` field.
--
-- IF EXISTS guards make this safe whether the original v2 migration ran
-- locally or not.
ALTER TABLE "user"
    DROP CONSTRAINT IF EXISTS user_onboarding_runtime_choice_check;

ALTER TABLE "user"
    DROP COLUMN IF EXISTS onboarding_runtime_skipped;

ALTER TABLE "user"
    DROP COLUMN IF EXISTS onboarding_runtime_id;
