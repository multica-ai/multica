-- The registered hook drains bulk duplicates in 128-row, 2s transactions.
-- Only a bounded residual from moves committed after the hook runs under the
-- DDL lock. Every statement has a finite execution cap; no comment DDL lock
-- spans the residual. Oversized/slow residuals abort atomically; retry the runner.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';
-- Drain search writers before collapsing projections. A concurrent move cannot
-- add a projection between the cleanup and the trigger-function replacement.
LOCK TABLE search_index_change IN ACCESS EXCLUSIVE MODE;
-- Check at most 129 discarded projections. The table scan itself is capped
-- by statement_timeout, including when there are no duplicates on a large table.
DO $residual$
BEGIN
 IF (SELECT count(*) FROM (
  SELECT 1 FROM search_index_change a WHERE EXISTS (
   SELECT 1 FROM search_index_change b
   WHERE a.entity_type=b.entity_type AND a.entity_id=b.entity_id
   AND (a.change_xid,a.workspace_id)<(b.change_xid,b.workspace_id)
  ) LIMIT 129
 ) remaining)>128 THEN
  RAISE EXCEPTION 'project move rollback residual exceeds 128 rows; retry the runner'
   USING ERRCODE='54000';
 END IF;
END
$residual$;
-- Any discarded projection may be unseen by a client in either workspace.
-- Mark the greatest discarded xid atomically with the delete, using the same
-- conservative xmin contract as retention pruning (there is no max(xid8)).
WITH candidates AS MATERIALIZED (
 SELECT a.ctid FROM search_index_change a WHERE EXISTS (
  SELECT 1 FROM search_index_change b
  WHERE a.entity_type=b.entity_type AND a.entity_id=b.entity_id
  AND (a.change_xid,a.workspace_id)<(b.change_xid,b.workspace_id)
 ) LIMIT 128
), deleted AS (
 DELETE FROM search_index_change a USING candidates c WHERE a.ctid=c.ctid
 RETURNING a.change_xid
)
INSERT INTO search_index_prune_mark(singleton,pruned_through_xid)
 SELECT true,change_xid FROM deleted ORDER BY change_xid DESC LIMIT 1
 ON CONFLICT(singleton) DO UPDATE SET pruned_through_xid=
  GREATEST(search_index_prune_mark.pruned_through_xid,EXCLUDED.pruned_through_xid);
ALTER TABLE search_index_change DROP CONSTRAINT IF EXISTS search_index_change_pkey;
-- Restore 561's upsert, not DELETE/INSERT without a unique key. Until 619 down
-- builds the old key, indexed writes fail closed (42P10) and must retry after
-- rollback finishes. They cannot create duplicates that wedge the next build.
CREATE OR REPLACE FUNCTION record_search_index_change()
RETURNS TRIGGER LANGUAGE plpgsql AS $function$
DECLARE changed_id uuid; changed_workspace_id uuid;
BEGIN
 IF TG_OP='DELETE' THEN changed_id:=OLD.id;changed_workspace_id:=OLD.workspace_id;
 ELSE changed_id:=NEW.id;changed_workspace_id:=NEW.workspace_id; END IF;
 INSERT INTO search_index_change(entity_type,entity_id,workspace_id,change_xid,changed_at)
 VALUES(TG_ARGV[0],changed_id,changed_workspace_id,pg_current_xact_id(),now())
 ON CONFLICT(entity_type,entity_id) DO UPDATE
  SET workspace_id=EXCLUDED.workspace_id,change_xid=EXCLUDED.change_xid,changed_at=EXCLUDED.changed_at;
 RETURN NULL;
END
$function$;
-- Take the comment-table DDL lock only after all whole-table work is complete.
DROP TRIGGER IF EXISTS trg_comment_update_search_index_change ON comment;
CREATE TRIGGER trg_comment_update_search_index_change
 AFTER UPDATE OF content,deleted_at,issue_id,created_at ON comment
 FOR EACH ROW
 WHEN (current_setting('multica.workspace_teardown',true) IS DISTINCT FROM 'on')
 EXECUTE FUNCTION record_search_index_change('comment');
