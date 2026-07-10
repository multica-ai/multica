-- Revert FIR-2698 model registry.
DROP TABLE IF EXISTS model_registry_change_request;
DROP TABLE IF EXISTS model_registry_version;
DROP TABLE IF EXISTS model_registry;
