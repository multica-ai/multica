-- Audit existing workspace slugs against the reserved-words list.
--
-- After the URL refactor, workspace URLs are /{slug}/... where slug must not
-- collide with frontend top-level routes (login, onboarding, api, etc.).
-- The CreateWorkspace handler now rejects new reserved slugs, but pre-refactor
-- data could already contain conflicting slugs. This migration fails loudly
-- so deploy is blocked until those workspaces are renamed or deleted.
--
-- Keep the slug list in sync with:
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
    -- Auth + onboarding
    'login', 'logout', 'signup', 'onboarding', 'invite', 'auth',
    -- Reserved for future platform routes
    'api', 'admin', 'settings', 'help', 'about', 'pricing', 'changelog',
    -- Next.js / hosting internals
    '_next', 'favicon.ico', 'robots.txt', 'sitemap.xml', 'manifest.json', '.well-known'
  );

  IF conflict_count > 0 THEN
    RAISE EXCEPTION 'Found % workspace(s) with reserved slugs: %. Rename or delete before deploying.', conflict_count, conflict_list;
  END IF;
END $$;
