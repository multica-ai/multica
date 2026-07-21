-- Preserve onboarding content on rollback. The origin_id is the member who
-- completed onboarding, so it is the safest legacy attribution once the issue
-- table can no longer represent a system creator. Comments and all user edits
-- remain intact.
UPDATE issue
SET creator_type = 'member',
    creator_id = origin_id,
    origin_type = NULL,
    origin_id = NULL
WHERE creator_type = 'system'
  AND origin_type IN (
      'onboarding_no_runtime_install',
      'onboarding_no_runtime_guide'
  )
  AND origin_id IS NOT NULL;

-- A future feature may add another system-issue provenance. Fail closed rather
-- than deleting or silently misattributing data the rollback does not understand.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM issue WHERE creator_type = 'system') THEN
        RAISE EXCEPTION 'cannot roll back system issue creator support: unsupported system-created issues remain';
    END IF;
END
$$;

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_creator_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_creator_type_check
    CHECK (creator_type IN ('member', 'agent')) NOT VALID;

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN (
        'autopilot',
        'quick_create',
        'lark_chat',
        'slack_chat',
        'agent_create'
    )) NOT VALID;
