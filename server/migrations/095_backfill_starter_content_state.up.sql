-- Backfill `starter_content_state` for users who finished onboarding between
-- the original 054 migration and the removal of the starter-content kit
-- (MUL-2438). 054 only covered pre-feature users; everyone onboarded in the
-- window since then could still be sitting at NULL. Old desktop clients gate
-- the legacy StarterContentPrompt on `starter_content_state IS NULL`, and the
-- /api/me/starter-content/import|dismiss routes no longer exist, so leaving
-- these rows NULL would surface a dialog whose buttons 404. Mark them
-- 'imported' to match the new helper's claim semantics.
UPDATE "user"
   SET starter_content_state = 'imported'
 WHERE onboarded_at IS NOT NULL
   AND starter_content_state IS NULL;
