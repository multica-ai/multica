-- Revert the partial unique index back to the unconditional constraint.
-- Self-healing: if rebinds have occurred in production there may be
-- (active, revoked) pairs sharing the same app_id. The unconditional
-- UNIQUE(app_id) can only be added back when at most one row per app_id
-- exists. We deduplicate revoked rows first — they have no active WS
-- connection and the history is not needed for the original constraint
-- model — then re-add the constraint. If revoked rows remain after the
-- delete (e.g. two revoked rows for the same app_id), keep the most
-- recently updated one and delete the rest before re-adding the
-- constraint.
DELETE FROM lark_installation
WHERE id IN (
    SELECT id FROM (
        SELECT id, ROW_NUMBER() OVER (
            PARTITION BY app_id
            ORDER BY
                CASE WHEN status = 'active' THEN 0 ELSE 1 END,
                updated_at DESC
        ) AS rn
        FROM lark_installation
    ) ranked
    WHERE rn > 1
);
DROP INDEX IF EXISTS lark_installation_app_id_active_key;
ALTER TABLE lark_installation ADD CONSTRAINT lark_installation_app_id_key UNIQUE (app_id);
