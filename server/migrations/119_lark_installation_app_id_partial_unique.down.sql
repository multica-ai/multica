-- Revert the partial unique index back to the unconditional constraint.
DROP INDEX IF EXISTS lark_installation_app_id_active_key;
ALTER TABLE lark_installation ADD CONSTRAINT lark_installation_app_id_key UNIQUE (app_id);
