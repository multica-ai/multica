-- Allow the no-runtime onboarding completion transaction to create two
-- platform-authored issues with durable provenance. Re-adding both checks as
-- NOT VALID avoids scanning the issue table while an ACCESS EXCLUSIVE lock is
-- held; PostgreSQL still enforces each check for every new or updated row.
-- Existing rows were already covered by the stricter constraints these replace.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_creator_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_creator_type_check
    CHECK (creator_type IN ('member', 'agent', 'system')) NOT VALID;

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN (
        'autopilot',
        'quick_create',
        'lark_chat',
        'slack_chat',
        'agent_create',
        'onboarding_no_runtime_install',
        'onboarding_no_runtime_guide'
    )) NOT VALID;
