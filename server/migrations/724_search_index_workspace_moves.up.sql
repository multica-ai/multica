-- Search changes belong to a workspace, not just an entity. A departure and
-- arrival must coexist; otherwise a source cursor silently loses the deletion.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';
-- USING INDEX renames the index. A completed-but-unrecorded retry must keep
-- the installed workspace key rather than drop it and reference its old name.
DO $key$
BEGIN
 IF NOT EXISTS (
  SELECT 1 FROM pg_constraint c
  WHERE c.conrelid='search_index_change'::regclass AND c.contype='p'
  AND c.conkey=ARRAY[
   (SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='workspace_id'),
   (SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='entity_type'),
   (SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='entity_id')
  ]::smallint[]
 ) THEN
  ALTER TABLE search_index_change DROP CONSTRAINT IF EXISTS search_index_change_pkey;
  DROP INDEX IF EXISTS search_index_change_pkey;
  ALTER TABLE search_index_change ADD CONSTRAINT search_index_change_pkey
   PRIMARY KEY USING INDEX idx_search_index_workspace_key;
 END IF;
END
$key$;
CREATE OR REPLACE FUNCTION record_search_index_change()
RETURNS TRIGGER LANGUAGE plpgsql AS $function$
DECLARE changed_id uuid; changed_workspace_id uuid;
BEGIN
 IF TG_OP='UPDATE' AND OLD.workspace_id IS DISTINCT FROM NEW.workspace_id THEN
  INSERT INTO search_index_change(entity_type,entity_id,workspace_id,change_xid,changed_at)
  VALUES(TG_ARGV[0],OLD.id,OLD.workspace_id,pg_current_xact_id(),now())
  ON CONFLICT(workspace_id,entity_type,entity_id) DO UPDATE
   SET change_xid=EXCLUDED.change_xid,changed_at=EXCLUDED.changed_at;
 END IF;
 IF TG_OP='DELETE' THEN
  changed_id:=OLD.id;changed_workspace_id:=OLD.workspace_id;
 ELSE
  changed_id:=NEW.id;changed_workspace_id:=NEW.workspace_id;
 END IF;
 INSERT INTO search_index_change(entity_type,entity_id,workspace_id,change_xid,changed_at)
 VALUES(TG_ARGV[0],changed_id,changed_workspace_id,pg_current_xact_id(),now())
 ON CONFLICT(workspace_id,entity_type,entity_id) DO UPDATE
  SET change_xid=EXCLUDED.change_xid,changed_at=EXCLUDED.changed_at;
 RETURN NULL;
END
$function$;
DROP TRIGGER IF EXISTS trg_comment_update_search_index_change ON comment;
CREATE TRIGGER trg_comment_update_search_index_change
 AFTER UPDATE OF content,deleted_at,issue_id,created_at,workspace_id ON comment
 FOR EACH ROW
 WHEN (current_setting('multica.workspace_teardown',true) IS DISTINCT FROM 'on')
 EXECUTE FUNCTION record_search_index_change('comment');
