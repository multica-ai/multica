-- Audit + rename existing workspace slugs against the newly-added reserved
-- set from MUL-961 (slug review follow-up).
--
-- This PR expands the reserved list in three directions:
--   * §1 Real conflict: `homepage` — `/homepage` is an active Next.js route
--     (`apps/web/app/(landing)/homepage/page.tsx`) that was missing from the
--     reserved list. Audit confirms zero prod workspaces with this slug.
--   * §3 Likely-future routes: home, dashboard, profile, account, billing,
--     notifications, search, members.
--   * API / ops prefixes: v1, v2, graphql, webhooks, sdk, tokens, cli,
--     health, ws, metrics, ping.
--
-- Per db-boy's prod audit (MUL-961 thread, 2026-04-22), two slugs in the §3
-- set already had live prod workspaces:
--
--   * `home`       (68a982da-68a7-4e2e-ac8e-45a0323507f3) — zzlye, 2026-04-14
--   * `dashboard`  (ea5a332f-06f9-480d-ab81-8f2324c92d80) — 王争,  2026-04-22
--
-- Decision on MUL-961: force-rename both via this migration (scheme 1), same
-- playbook as MUL-972 for admin/multica/new/www. Rename targets `home-1`
-- and `dashboard-1` were verified unoccupied at audit time. The subsequent
-- DO block is a generic fallback that picks `<slug>-N` for any other row
-- that slips in between audit and deploy (defensive against a race with new
-- workspace creation — the reserved-slug check in app code lands in the
-- same deploy, but the migration runs first).
--
-- Owner outreach: zzlye@ and 王争@ should be notified that their
-- workspace URL prefix changed (/home → /home-1, /dashboard → /dashboard-1).
--
-- Keep this slug list aligned with:
--  - server/internal/handler/workspace_reserved_slugs.go
--  - packages/core/paths/reserved-slugs.ts

-- 1. Targeted renames for the two known conflicts at audit time.
UPDATE workspace SET slug = 'home-1'
  WHERE id = '68a982da-68a7-4e2e-ac8e-45a0323507f3' AND slug = 'home';
UPDATE workspace SET slug = 'dashboard-1'
  WHERE id = 'ea5a332f-06f9-480d-ab81-8f2324c92d80' AND slug = 'dashboard';

-- 2. Generic fallback: any other row whose slug lands in the newly
-- reserved set (race or new data between audit and deploy) is renamed to
-- `<slug>-N` with the lowest N that is free. Same pattern as the existing
-- audit migrations, hardened against collisions.
DO $$
DECLARE
  r RECORD;
  n INT;
BEGIN
  FOR r IN
    SELECT id, slug FROM workspace
    WHERE slug IN (
      -- Real conflict fix
      'homepage',
      -- Platform / marketing (newly added)
      'home', 'dashboard',
      -- Account / billing (newly added)
      'profile', 'account', 'billing', 'notifications', 'search', 'members',
      -- API / integration prefixes (newly added)
      'v1', 'v2', 'graphql', 'webhooks', 'sdk', 'tokens', 'cli',
      -- Backend ops / observability (newly added)
      'health', 'ws', 'metrics', 'ping'
    )
  LOOP
    n := 1;
    WHILE EXISTS (SELECT 1 FROM workspace WHERE slug = r.slug || '-' || n) LOOP
      n := n + 1;
    END LOOP;
    UPDATE workspace SET slug = r.slug || '-' || n WHERE id = r.id;
    RAISE NOTICE 'Renamed workspace % slug from % to %', r.id, r.slug, r.slug || '-' || n;
  END LOOP;
END $$;

-- 3. Post-condition audit: no workspace should remain on a reserved slug.
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
    'homepage',
    'home', 'dashboard',
    'profile', 'account', 'billing', 'notifications', 'search', 'members',
    'v1', 'v2', 'graphql', 'webhooks', 'sdk', 'tokens', 'cli',
    'health', 'ws', 'metrics', 'ping'
  );

  IF conflict_count > 0 THEN
    RAISE EXCEPTION 'After rename pass, % workspace(s) still on reserved slugs: %. This should be impossible — investigate.', conflict_count, conflict_list;
  END IF;
END $$;
