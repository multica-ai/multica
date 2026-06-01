-- FIR-2596 — queries for the Google Workspace → cerebro_group sync worker.
-- These are sync-only paths kept separate from the human-facing group.sql
-- CRUD: they key on google_group_email and carry the member `source` column
-- so reconciliation only ever touches members the sync itself created.

-- name: UpsertCerebroGroupByGoogleEmail :one
-- Find-or-create a group by its Google email. The display name is taken from
-- the fixed FIR-2596 list, not from BigQuery, so an existing synced group is
-- left with whatever name it already has (a human rename in the UI sticks).
INSERT INTO cerebro_group (workspace_id, name, google_group_email)
VALUES ($1, $2, $3)
ON CONFLICT (workspace_id, google_group_email) WHERE google_group_email IS NOT NULL
DO UPDATE SET updated_at = now()
RETURNING id, workspace_id, name, description, google_group_email, created_by, created_at, updated_at;

-- name: ListCerebroGroupMembersBySource :many
-- The user_ids currently in a group that were added by a given source. The
-- worker reads source='google_workspace' to know which members it owns and
-- may remove; manual members are invisible to it.
SELECT user_id FROM cerebro_group_member
WHERE group_id = $1 AND source = $2;

-- name: AddCerebroGroupMemberWithSource :exec
-- Idempotent insert that stamps provenance. ON CONFLICT DO NOTHING means a
-- member already present (manual or synced) is left untouched — we never
-- downgrade a manual membership to a synced one.
INSERT INTO cerebro_group_member (group_id, user_id, source)
VALUES ($1, $2, $3)
ON CONFLICT (group_id, user_id) DO NOTHING;

-- name: RemoveCerebroGroupMemberBySource :exec
-- Remove a member only if it belongs to the given source. Guards against the
-- worker ever deleting a hand-added member that happens to share the user_id.
DELETE FROM cerebro_group_member
WHERE group_id = $1 AND user_id = $2 AND source = $3;
