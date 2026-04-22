-- Audit existing workspace slugs against the extended reserved-slug list.
--
-- This PR (1) renames the global workspace-creation route from /new-workspace
-- to /workspaces/new, which moves the reserved name from "new-workspace" to
-- "workspaces", and (2) expands the reserved slug list to cover the broader
-- set recommended by the URL-design audit (auth flow words, RFC 2142 mailbox
-- names, hostname confusables, common platform routes).
--
-- Migration 046 was REMOVED in the same PR. It was auditing "new-workspace"
-- which is no longer reserved, so it had become dead code AND was actively
-- blocking prd deploy on a real-user workspace that no longer needs renaming
-- (the workspace is now safe under the new route — `new-workspace` slug
-- resolves to its workspace, no longer shadowed by the global route which
-- moved to /workspaces/new). Removing 046 is forward-only safe: 046 had
-- never successfully applied in prd (it was the source of the deploy
-- block), and the audit-only nature means down-rollback is a no-op.
--
-- The data audit was performed before this migration was written and confirmed
-- ZERO conflicts for every slug listed below in production. This migration
-- exists as a safety net: if a workspace with one of these slugs slips into
-- prod between audit and deploy, the migration will fail loudly rather than
-- silently shadowing the workspace behind a system route.
--
-- Slugs INTENTIONALLY OMITTED from this audit despite being in the reserved
-- list: 'admin', 'multica', 'new', 'setup', 'www'. These already have one
-- conflicting workspace each in production. They will be handled in a
-- follow-up PR (rename via owner outreach + targeted migration), not blocked
-- on this deploy.
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
    -- Auth flow (newly added)
    'signin', 'signout', 'oauth', 'callback', 'verify', 'reset', 'password',

    -- Platform routes (newly added)
    'docs', 'support', 'status', 'legal', 'privacy', 'terms', 'security',
    'contact', 'blog', 'careers', 'press', 'download',

    -- Workspace/team segments (newly added — replaces 'new-workspace')
    'workspaces', 'teams',

    -- RFC 2142 mailboxes (newly added)
    'postmaster', 'abuse', 'noreply', 'webmaster', 'hostmaster',

    -- Hostname confusables (newly added)
    'mail', 'ftp', 'static', 'cdn', 'assets', 'public', 'files', 'uploads'
  );

  IF conflict_count > 0 THEN
    RAISE EXCEPTION 'Found % workspace(s) with slugs that collide with extended reserved list: %. Rename or delete before deploying.', conflict_count, conflict_list;
  END IF;
END $$;
