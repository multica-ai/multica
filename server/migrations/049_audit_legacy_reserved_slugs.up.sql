-- Audit `admin`, `multica`, `new`, `www` against the workspace.slug column.
--
-- Follow-up to migration 047 (extended reserved slugs). 047 intentionally
-- omitted these four slugs from its audit because each had one conflicting
-- workspace in production at the time, and blocking deploy on owner outreach
-- was deemed unacceptable. MUL-972 closed that loop on prd:
--
--   * `admin`   (99cd10e4-…) → renamed to `legacy-admin-99cd10e4`
--   * `multica` (dcd796aa-…) → renamed to `legacy-multica-dcd796aa`
--   * `new`     (e391e3ed-…) → renamed to `legacy-new-e391e3ed`
--   * `www`     (5e8d38b2-…) → workspace deleted (was empty: 0 issues /
--                              projects / agents, owner-only member)
--
-- With the prd data clean, this migration promotes those four slugs into the
-- audit set so any future workspace that slips in with one of them fails
-- startup loudly instead of being silently shadowed by a global route.
--
-- `setup` is STILL omitted from this audit. The `setup` workspace
-- (b43f0bc2-…) is a real production user (Roberto Betancourth, building a
-- chants/Alabanzas app) and is being handled out-of-band via owner outreach
-- to negotiate a rename. A separate follow-up migration will fold `setup`
-- into the audit once that workspace's slug has been migrated.
--
-- Keep this slug list aligned with:
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
    'admin',
    'multica',
    'new',
    'www'
  );

  IF conflict_count > 0 THEN
    RAISE EXCEPTION 'Found % workspace(s) with slugs that collide with the legacy reserved-slug audit set: %. Rename or delete before deploying (see MUL-972 for the playbook).', conflict_count, conflict_list;
  END IF;
END $$;
