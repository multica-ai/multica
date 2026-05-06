-- Backfill onboarded_at for users who already belong to a workspace.
-- PR #1868 (since reverted) routed users by hasWorkspace instead of onboarded_at,
-- producing a population of users with workspace memberships but
-- onboarded_at == NULL. After the new design enforces
-- "member row exists ↔ onboarded_at != null" via backend transactions,
-- this one-shot backfill cleans existing dirty rows.
--
-- Uses created_at as the timestamp because these users were de facto onboarded
-- when their account was first created — backfilling with now() would distort
-- onboarding-funnel analytics. COALESCE keeps it idempotent.
UPDATE "user"
SET onboarded_at = COALESCE(onboarded_at, created_at)
WHERE id IN (SELECT DISTINCT user_id FROM member);
