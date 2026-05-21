-- Inverse of 093_onboarding_questionnaire_v2.up.sql. Lossy: `source`
-- (any value), per-question skip markers, and the v2-specific
-- use_case values (personal_tasks / plan_research / automate_ops)
-- have no v1 equivalent. We map what we can; remaining slots stay
-- null. This is acceptable — we rarely run migrate-down against prod.

UPDATE "user"
SET onboarding_questionnaire = jsonb_strip_nulls(jsonb_build_object(
        'team_size', NULL,
        'team_size_other', NULL,
        'role', CASE onboarding_questionnaire->>'role'
            WHEN 'engineer' THEN 'developer'
            WHEN 'product'  THEN 'product_lead'
            WHEN 'writer'   THEN 'writer'
            WHEN 'founder'  THEN 'founder'
            WHEN 'other'    THEN 'other'
            ELSE NULL
        END,
        'role_other', onboarding_questionnaire->>'role_other',
        'use_case', CASE onboarding_questionnaire->>'use_case'
            WHEN 'ship_code'      THEN 'coding'
            WHEN 'manage_team'    THEN 'planning'
            WHEN 'write_publish'  THEN 'writing_research'
            WHEN 'evaluate'       THEN 'explore'
            WHEN 'other'          THEN 'other'
            ELSE NULL
        END,
        'use_case_other', onboarding_questionnaire->>'use_case_other'
    ))
WHERE onboarding_questionnaire IS NOT NULL
  AND onboarding_questionnaire->>'version' = '2';
