-- FIR-1412 rollback: drop the entity-folder tables. The membership table is
-- dropped first (it FKs the folder table).
DROP TABLE IF EXISTS cerebro_entity_folder_item;
DROP TABLE IF EXISTS cerebro_entity_folder;
