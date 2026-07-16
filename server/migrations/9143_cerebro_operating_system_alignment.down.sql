-- Refuse to destroy standalone Rocks or check-in history during rollback.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM cerebro_rock WHERE project_id IS NULL) THEN
        RAISE EXCEPTION 'cannot roll back FIR-2816 alignment while standalone Rocks exist';
    END IF;
    IF EXISTS (SELECT 1 FROM cerebro_rock_check_in) THEN
        RAISE EXCEPTION 'cannot roll back FIR-2816 alignment while Rock check-ins exist';
    END IF;
END $$;

DELETE FROM cerebro_object_connection
WHERE source_type = 'rock'
  AND target_type = 'project'
  AND provenance = 'system'
  AND created_by_type = 'system';

DROP TRIGGER cerebro_strategy_item_history_trigger ON cerebro_strategy_item;
DROP FUNCTION cerebro_record_strategy_item_history();
DROP TABLE cerebro_strategy_item_history;
DROP TABLE cerebro_rock_check_in;

UPDATE cerebro_object_connection c
SET target_id = r.project_id
FROM cerebro_rock r
WHERE c.workspace_id = r.workspace_id
  AND c.target_type = 'rock'
  AND c.target_id = r.id
  AND r.project_id IS NOT NULL;

DROP INDEX idx_cerebro_rock_workspace_period_v2;
DROP INDEX idx_cerebro_rock_legacy_project;

ALTER TABLE cerebro_object_connection
    DROP CONSTRAINT cerebro_object_connection_created_by_type_check,
    ADD CONSTRAINT cerebro_object_connection_created_by_type_check
        CHECK (created_by_type IN ('member', 'agent'));

ALTER TABLE cerebro_rock
    DROP CONSTRAINT cerebro_rock_pkey,
    DROP CONSTRAINT cerebro_rock_owner_type_check,
    DROP CONSTRAINT cerebro_rock_title_not_blank,
    ALTER COLUMN project_id SET NOT NULL,
    ADD CONSTRAINT cerebro_rock_pkey PRIMARY KEY (project_id),
    DROP COLUMN period_id,
    DROP COLUMN owner_id,
    DROP COLUMN owner_type,
    DROP COLUMN description,
    DROP COLUMN title,
    DROP COLUMN id;

ALTER TABLE cerebro_strategy_item DROP COLUMN horizon_label;
DROP TABLE cerebro_operating_period;
