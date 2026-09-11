-- Reserve the `triage` status key (MUL-7212, design MUL-7189 §2.1).
--
-- `triage` becomes a platform-reserved key: the status of an issue waiting in
-- Triage. It has no catalog row, it is not a category, and the server refuses
-- it as a custom key (issuestatus.IsBuiltIn). Before this change nothing
-- reserved it, so a workspace may already own a CUSTOM status keyed `triage`;
-- once the new server reads that key as "in Triage", every issue on it would
-- silently change meaning.
--
-- This migration runs before any server that reserves the key starts (the
-- entrypoint finishes migrations first). The file executes as one implicit
-- transaction, and for every workspace that owns a custom `triage` it:
--
--   1. picks a replacement key with the same rule as issuestatus.firstFreeKey:
--      `triage_2`, `triage_3`, ... — the first one this workspace does not own,
--      counting archived rows, because the (workspace_id, key) index is not
--      partial. A fixed name such as `triage_legacy` could collide;
--   2. renames the catalog row, moves the workspace's issues on `triage` to
--      the replacement, and rewrites `triage` inside saved views'
--      query.statusFilters — the only other place a status key is stored as
--      live configuration. activity_log / inbox_item details are history and
--      are left as written;
--   3. prints one NOTICE per workspace with the counts.
--
-- Each workspace's catalog is taken under the same EXCLUSIVE advisory lock
-- that archive holds. Old pods still running during the rollout take the
-- SHARED side and re-resolve a custom status before writing an issue onto it,
-- so an in-flight write either commits before the rename (and is moved with
-- the rest) or re-resolves after it and is refused. No issue can be left on
-- the old key.
--
-- Finally a CHECK makes the reservation hold in storage, so an old pod that
-- still accepts `triage` as a custom key cannot recreate the conflict.
--
-- issue.revision is bumped on the moved issues, so a client editing with the
-- old status in hand gets a revision conflict instead of writing it back.
-- The rename is not reversed by the down migration; the replacement key is an
-- ordinary custom key the admin can keep or rename.

DO $$
DECLARE
    legacy RECORD;
    n INT;
    replacement TEXT;
    moved_issues BIGINT;
    rewritten_views BIGINT;
BEGIN
    FOR legacy IN
        SELECT id, workspace_id FROM issue_status WHERE key = 'triage' ORDER BY workspace_id
    LOOP
        PERFORM pg_advisory_xact_lock(hashtextextended(legacy.workspace_id::text || ':issue_status', 0));

        n := 2;
        WHILE EXISTS (
            SELECT 1 FROM issue_status
            WHERE workspace_id = legacy.workspace_id AND key = 'triage_' || n
        ) LOOP
            n := n + 1;
        END LOOP;
        replacement := 'triage_' || n;

        UPDATE issue_status
        SET key = replacement, updated_at = now()
        WHERE id = legacy.id;

        UPDATE issue
        SET status = replacement, revision = revision + 1
        WHERE workspace_id = legacy.workspace_id AND status = 'triage';
        GET DIAGNOSTICS moved_issues = ROW_COUNT;

        UPDATE issue_view
        SET query = jsonb_set(query, '{statusFilters}', (
                SELECT jsonb_agg(
                    CASE WHEN f.value = to_jsonb('triage'::text) THEN to_jsonb(replacement) ELSE f.value END
                    ORDER BY f.ordinality)
                FROM jsonb_array_elements(query -> 'statusFilters') WITH ORDINALITY AS f(value, ordinality)
            )),
            revision = revision + 1,
            updated_at = now()
        WHERE workspace_id = legacy.workspace_id
          AND jsonb_typeof(query -> 'statusFilters') = 'array'
          AND query -> 'statusFilters' @> '["triage"]'::jsonb;
        GET DIAGNOSTICS rewritten_views = ROW_COUNT;

        RAISE NOTICE 'reserve triage status key: workspace % renamed custom status triage to %, moved % issue(s), rewrote % saved view(s)',
            legacy.workspace_id, replacement, moved_issues, rewritten_views;
    END LOOP;
END $$;

ALTER TABLE issue_status
    ADD CONSTRAINT issue_status_key_not_reserved CHECK (key <> 'triage');
