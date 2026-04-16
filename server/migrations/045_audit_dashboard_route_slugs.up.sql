-- Audit existing workspace slugs against the dashboard route segment names.
--
-- Migration 043 audited the auth/onboarding/hosting reserved words. This one
-- adds the dashboard route segments (issues / projects / agents / inbox /
-- my-issues / runtimes / skills / autopilots) — slug = any of these would
-- produce visually ambiguous URLs after the URL refactor (e.g. /issues/abc
-- could mean "issue abc in workspace 'issues'" or "issue abc in some
-- workspace"). Reserve to avoid the confusion.
--
-- "settings" was already reserved by 043, no need to repeat.
--
-- Keep this slug list in sync with:
--  - server/internal/handler/workspace_reserved_slugs.go
--  - packages/core/paths/reserved-slugs.ts

DO $$
DECLARE
  conflict_count INT;
  conflict_list TEXT;
BEGIN
  SELECT
    COUNT(*),
    string_agg(slug, ', ' ORDER BY slug)
  INTO conflict_count, conflict_list
  FROM workspace
  WHERE slug IN (
    'issues', 'projects', 'autopilots', 'agents',
    'inbox', 'my-issues', 'runtimes', 'skills'
  );

  IF conflict_count > 0 THEN
    RAISE EXCEPTION 'Found % workspace(s) with slugs that collide with dashboard routes: %. Rename or delete before deploying.', conflict_count, conflict_list;
  END IF;
END $$;
