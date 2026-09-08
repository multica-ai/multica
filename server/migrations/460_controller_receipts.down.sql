DROP TRIGGER controller_status_definition_guard ON issue_status;
DROP TRIGGER controller_property_definition_guard ON issue_property;
DROP FUNCTION enforce_controller_definition();
DROP TABLE controller_effect_operation;
ALTER TABLE controlled_run DROP COLUMN request_hash;
