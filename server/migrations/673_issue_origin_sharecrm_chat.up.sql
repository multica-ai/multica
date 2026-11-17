-- Record the ShareCRM origin widen under the filename this release ships.
--
-- Older releases used these full ledger keys for the same logical change.
-- The runner does not delete them, and the files must not come back: a
-- database that never recorded that exact string would execute them again,
-- and 649/657/661/665 are now different upstream migrations.
--   649_issue_origin_sharecrm_chat / 650_issue_origin_sharecrm_chat_validate
--   657_issue_origin_sharecrm_chat / 658_issue_origin_sharecrm_chat_validate
--   661_issue_origin_sharecrm_chat / 662_issue_origin_sharecrm_chat_validate
--   665_issue_origin_sharecrm_chat / 666_issue_origin_sharecrm_chat_validate
--
-- 667/668 on this fork already union sharecrm_chat in, including on a
-- database whose 667 committed before that repair existed (668 reads the
-- rows and puts the value back). This file is then a catalog no-op: it
-- does not take ACCESS EXCLUSIVE and does not schedule another scan.
-- A database that already validated upstream 667/668 (no sharecrm_chat)
-- still widens here. Migration 674 validates. No table scan in this file.
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
