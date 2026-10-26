DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM task_token WHERE is_aido_owner)
    OR EXISTS (SELECT 1 FROM aido_owner_binding) THEN
   RAISE EXCEPTION 'reconcile AIDO scoped tokens and bindings before rollback';
 END IF;
END $$;
DROP TRIGGER aido_agent_runtime_guard ON agent;
DROP FUNCTION preserve_aido_runtime_binding();
DROP TRIGGER aido_token_scope_guard ON task_token;
DROP FUNCTION enforce_aido_task_token_scope();
DROP TRIGGER aido_owner_binding_guard ON aido_owner_binding;
DROP FUNCTION enforce_aido_owner_binding();
DROP TRIGGER aido_binding_delete_guard ON aido_owner_binding;
DROP FUNCTION preserve_aido_binding_on_delete();
ALTER TABLE task_token DROP COLUMN is_aido_owner;
DROP TABLE aido_owner_binding;
