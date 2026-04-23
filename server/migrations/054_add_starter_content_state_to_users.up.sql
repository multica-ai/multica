-- Post-onboarding starter content opt-in. State values:
--   NULL           → we haven't asked the user yet (new user just after
--                    onboarding). The workspace issues page shows the
--                    StarterContentPrompt dialog until they decide.
--   'imported'     → user chose to import; seeding ran; never ask again.
--   'dismissed'    → user declined; never ask again.
--   'skipped_legacy' → backfilled for pre-feature users so they aren't
--                    prompted when they next sign in.
ALTER TABLE "user" ADD COLUMN starter_content_state TEXT;

-- Anyone who already finished onboarding before this feature existed
-- should NOT see the prompt — their workspace has already settled into
-- whatever state they wanted.
UPDATE "user"
   SET starter_content_state = 'skipped_legacy'
 WHERE onboarded_at IS NOT NULL
   AND starter_content_state IS NULL;
