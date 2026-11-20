-- Record the ShareCRM origin widen under the filename this release ships.
--
-- Older releases used these full ledger keys for the same logical change.
-- The runner does not delete them, and the files must not come back: a
-- database that never recorded that exact string would execute them again,
-- and 649/657/661/665/673/674/681/682/683/684/685/686/695/696/697/698/703/704
-- are now different migrations.
--   649_issue_origin_sharecrm_chat / 650_issue_origin_sharecrm_chat_validate
--   657_issue_origin_sharecrm_chat / 658_issue_origin_sharecrm_chat_validate
--   661_issue_origin_sharecrm_chat / 662_issue_origin_sharecrm_chat_validate
--   665_issue_origin_sharecrm_chat / 666_issue_origin_sharecrm_chat_validate
--   673_issue_origin_sharecrm_chat / 674_issue_origin_sharecrm_chat_validate
--   681_issue_origin_sharecrm_chat / 682_issue_origin_sharecrm_chat_validate
--   683_issue_origin_sharecrm_chat / 684_issue_origin_sharecrm_chat_validate
--   685_issue_origin_sharecrm_chat / 686_issue_origin_sharecrm_chat_validate
--   695_issue_origin_sharecrm_chat / 696_issue_origin_sharecrm_chat_validate
--   697_issue_origin_sharecrm_chat / 698_issue_origin_sharecrm_chat_validate
--   703_issue_origin_sharecrm_chat / 704_issue_origin_sharecrm_chat_validate
--
-- 681/682 on upstream are project_folder and its name index. 683/684 are
-- entitlement_feature_attempt and its scope index. 685-694 are the docs
-- tables and their keys. 695/696 are inbox_item.returned_at and
-- idx_inbox_active_shown. 697/698 are autopilot_run.idempotency_key and
-- uq_autopilot_run_idempotency_key. 699 widens member and invitation roles
-- for guests. 700-702 are member_search_access and its primary key.
-- 703-707 are doc_comment, its primary key, and its doc and workspace
-- indexes. 708/709 build idx_attachment_workspace_size and drop
-- idx_attachment_workspace. 710 adds
-- autopilot_quota_period.rejection_notified_limit.
-- A database that already recorded the ShareCRM filenames above still runs
-- this file: the ledger key is the whole filename, not the numeric prefix.
-- When weixin_chat and sharecrm_chat are already allowed, including when a
-- retired widen left the check NOT VALID, this file returns without another
-- catalog swap. Migration 712 validates.
--
-- 667/668 on this fork already union sharecrm_chat in, including on a
-- database whose 667 committed before that repair existed (668 reads the
-- rows and puts the value back). This file is then a catalog no-op: it
-- does not take ACCESS EXCLUSIVE and does not schedule another scan.
-- A database that already validated upstream 667/668 (no sharecrm_chat)
-- still widens here. No table scan in this file.
DO $$
DECLARE
  def text;
  members text[];
  item text;
  list text;
  required text[] := ARRAY['weixin_chat', 'sharecrm_chat'];
BEGIN
  SELECT pg_get_constraintdef(c.oid)
    INTO def
  FROM pg_constraint c
  WHERE c.conrelid = to_regclass('issue')
    AND c.conname = 'issue_origin_type_check';
  IF def IS NULL OR position('origin_type' IN def) = 0 THEN
    RAISE EXCEPTION 'issue_origin_type_check missing or unexpected: %', def;
  END IF;

  SELECT COALESCE(array_agg(replace(m[1], '''''', '''') ORDER BY ord), ARRAY[]::text[])
    INTO members
  FROM regexp_matches(def, '''((?:[^'']|'''')*)''', 'g') WITH ORDINALITY AS match(m, ord);
  IF members IS NULL OR cardinality(members) = 0 THEN
    RAISE EXCEPTION 'could not read origin values from %', def;
  END IF;

  IF 'weixin_chat' = ANY(members) AND 'sharecrm_chat' = ANY(members) THEN
    RAISE NOTICE 'migration report: origin check already allows weixin_chat and sharecrm_chat';
    RETURN;
  END IF;

  FOREACH item IN ARRAY required LOOP
    IF NOT item = ANY(members) THEN
      members := array_append(members, item);
    END IF;
  END LOOP;

  SELECT string_agg(quote_literal(v), ', ' ORDER BY ord)
    INTO list
  FROM unnest(members) WITH ORDINALITY AS u(v, ord);
  EXECUTE 'ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check';
  EXECUTE 'ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check CHECK (origin_type IN (' || list || ')) NOT VALID';
  RAISE NOTICE 'migration report: widened issue origin check without dropping existing origins';
END $$;
