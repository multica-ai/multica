DROP TABLE IF EXISTS workflow_transition;
DROP TABLE IF EXISTS workflow_output;
DROP TABLE IF EXISTS workflow_node_attempt;
DROP TABLE IF EXISTS workflow_node_activation;
DROP TABLE IF EXISTS workflow_scope_instance;
ALTER TABLE workflow_work_item DROP COLUMN IF EXISTS activation_id;
