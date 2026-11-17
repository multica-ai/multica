-- Add weixin_chat without removing origins this database already allows.
--
-- ShareCRM releases recorded the same widen under older full filenames
-- (649/650, 657/658, 661/662, 665/666). schema_migrations keys on that
-- whole filename, so those rows are not 673/674 and the files are gone.
-- The rows can still be in the table. A hardcoded weixin-only list drops
-- sharecrm_chat (and any operator-added origin). Migration 668 then fails
-- VALIDATE with 23514 after this file has already committed, and 681 never
-- runs.
--
-- This statement only adds weixin_chat and sharecrm_chat when they are
-- missing. It does not scan issue: the previous check was already validated,
-- so every existing origin is still named here. ACCESS EXCLUSIVE covers
-- only the catalog swap. Migration 668 validates.
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
