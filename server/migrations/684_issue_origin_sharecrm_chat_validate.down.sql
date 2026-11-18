-- PostgreSQL cannot mark a validated constraint NOT VALID. Recreate the
-- current allowed set without validating it. Keep sharecrm_chat. No scan.
DO $$
DECLARE
  def text;
  validated boolean;
  members text[];
  list text;
BEGIN
  SELECT c.convalidated, pg_get_constraintdef(c.oid)
    INTO validated, def
  FROM pg_constraint c
  WHERE c.conrelid = to_regclass('issue')
    AND c.conname = 'issue_origin_type_check';
  IF def IS NULL OR position('origin_type' IN def) = 0 THEN
    RAISE EXCEPTION 'issue_origin_type_check missing or unexpected: %', def;
  END IF;
  IF NOT validated THEN
    RAISE NOTICE 'migration report: issue origin check is already not validated';
    RETURN;
  END IF;

  SELECT COALESCE(array_agg(replace(m[1], '''''', '''') ORDER BY ord), ARRAY[]::text[])
    INTO members
  FROM regexp_matches(def, '''((?:[^'']|'''')*)''', 'g') WITH ORDINALITY AS match(m, ord);
  IF members IS NULL OR cardinality(members) = 0 THEN
    RAISE EXCEPTION 'could not read origin values from %', def;
  END IF;

  SELECT string_agg(quote_literal(v), ', ' ORDER BY ord)
    INTO list
  FROM unnest(members) WITH ORDINALITY AS u(v, ord);
  EXECUTE 'ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check';
  EXECUTE 'ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check CHECK (origin_type IN (' || list || ')) NOT VALID';
END $$;
