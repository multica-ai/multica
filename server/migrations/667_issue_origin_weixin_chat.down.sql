-- Remove only weixin_chat. sharecrm_chat and any other allowed origin stay.
-- VALIDATE shares this transaction with the catalog swap, so a failure
-- (23514 while weixin_chat rows remain) rolls the check back unchanged.
-- That scan holds ACCESS EXCLUSIVE. Rollback is not the unattended upgrade.
DO $$
DECLARE
  def text;
  members text[];
  list text;
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

  members := array_remove(members, 'weixin_chat');
  IF cardinality(members) = 0 THEN
    RAISE EXCEPTION 'refusing to remove the last issue origin value';
  END IF;

  SELECT string_agg(quote_literal(v), ', ' ORDER BY ord)
    INTO list
  FROM unnest(members) WITH ORDINALITY AS u(v, ord);
  EXECUTE 'ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check';
  EXECUTE 'ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check CHECK (origin_type IN (' || list || ')) NOT VALID';
  EXECUTE 'ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check';
END $$;
