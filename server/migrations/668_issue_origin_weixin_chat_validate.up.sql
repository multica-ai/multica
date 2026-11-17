-- Validate the check from migration 667.
--
-- Databases that already committed the upstream 667 file replaced the check
-- with weixin_chat and not sharecrm_chat. VALIDATE then raises 23514, this
-- version is not recorded, and 673 never runs. New sharecrm_chat writes are
-- already rejected. Repair that check before scanning.
--
-- When weixin_chat and sharecrm_chat are both already allowed, the repair
-- does not read issue and does not take ACCESS EXCLUSIVE. The following
-- VALIDATE is the only scan, and it takes SHARE UPDATE EXCLUSIVE.
--
-- When a required origin is missing, also read DISTINCT origin_type. Rows
-- left behind by the narrower NOT VALID check (sharecrm_chat, or an origin
-- an operator had allowed) must be named again or VALIDATE still fails.
-- Origins with no remaining rows cannot be reconstructed. That read holds
-- ACCESS SHARE and then the catalog swap upgrades it to ACCESS EXCLUSIVE.
-- COMMIT releases that lock before VALIDATE, so the scan does not block
-- ordinary reads and writes.
DO $$
DECLARE
  def text;
  members text[];
  additions text[];
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

  SELECT COALESCE(array_agg(DISTINCT origin_type), ARRAY[]::text[])
    INTO additions
  FROM issue
  WHERE origin_type IS NOT NULL AND origin_type <> '';
  additions := additions || required;
  FOREACH item IN ARRAY additions LOOP
    IF item IS NOT NULL AND item <> '' AND NOT item = ANY(members) THEN
      members := array_append(members, item);
    END IF;
  END LOOP;

  SELECT string_agg(quote_literal(v), ', ' ORDER BY ord)
    INTO list
  FROM unnest(members) WITH ORDINALITY AS u(v, ord);
  EXECUTE 'ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check';
  EXECUTE 'ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check CHECK (origin_type IN (' || list || ')) NOT VALID';
  RAISE NOTICE 'migration report: restored issue origins removed by an already-committed 667 before validation';
END $$;
-- Close the repair transaction before the scan. See the header comment.
COMMIT;
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
