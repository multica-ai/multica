DROP INDEX IF EXISTS uq_artifact_note_type_period;
ALTER TABLE artifact DROP COLUMN IF EXISTS period_key;
ALTER TABLE artifact DROP COLUMN IF EXISTS note_type_id;
DROP INDEX IF EXISTS idx_cerebro_note_type_sweep;
DROP INDEX IF EXISTS idx_cerebro_note_type_workspace;
DROP TABLE IF EXISTS cerebro_note_type;
