-- Rollback for 9046_cerebro_status_models.up.sql.
-- Drop the project link (child) before the model (parent).

DROP TABLE IF EXISTS cerebro_project_status_model;
DROP TABLE IF EXISTS cerebro_status_model;
