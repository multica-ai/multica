-- Cleanup for the "workspace" auto-generated slug fallback bug.
--
-- The frontend's nameToWorkspaceSlug() previously fell back to the literal
-- string "workspace" when the input name produced no valid slug characters
-- (Chinese / Japanese / emoji / Arabic names all stripped to empty by the
-- /[^a-z0-9]+/g regex). This meant the first non-ASCII-named workspace on
-- any instance silently got slug = "workspace" and (a) showed a confusing
-- /workspace/{view} URL after the URL refactor, (b) blocked subsequent
-- non-ASCII-named workspaces with 409 conflicts on the unique slug index.
--
-- The frontend bug is fixed in
--   packages/views/workspace/slug.ts (commit d5c9613f)
-- but pre-existing data with slug = 'workspace' must be migrated. Renaming
-- to 'workspace-<8 hex chars>' preserves URL stability for the few users
-- already on it while ensuring uniqueness if multiple instances had this
-- workspace (e.g. local dev DBs across the team).
--
-- For other broken slugs (numeric-only, emoji-only, etc.) we don't migrate
-- here because they are valid per the regex and the user might have chosen
-- them intentionally. Only the literal "workspace" fallback is patched.

UPDATE workspace
SET slug = 'workspace-' || substring(replace(id::text, '-', '') from 1 for 8)
WHERE slug = 'workspace';
