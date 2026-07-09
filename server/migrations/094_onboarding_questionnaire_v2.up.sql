-- Onboarding questionnaire schema v1 → v2 migration.
--
-- v1 shape: { team_size, team_size_other, role, role_other, use_case, use_case_other }
-- v2 shape: { source, source_*, role, role_*, use_case, use_case_*, version: 2 }
--
-- `team_size` is dropped entirely. `role` and `use_case` are remapped
-- to the new enum vocabulary. Historical nulls stay null (NOT marked
-- `*_skipped: true` — the v1 flow forbade skips, so attributing skip
-- intent retroactively would pollute analytics). `source` is null for
-- everyone (we never asked).

UPDATE "user"
SET onboarding_questionnaire = jsonb_strip_nulls(jsonb_build_object(
        'source', NULL,
        'source_other', NULL,
        'source_skipped', false,
        'role', CASE onboarding_questionnaire->>'role'
            WHEN 'developer'    THEN 'engineer'
            WHEN 'product_lead' THEN 'product'
            WHEN 'writer'       THEN 'writer'
            WHEN 'founder'      THEN 'founder'
            WHEN 'other'        THEN 'other'
            ELSE NULL
        END,
        'role_other', onboarding_questionnaire->>'role_other',
        'role_skipped', false,
        'use_case', CASE onboarding_questionnaire->>'use_case'
            WHEN 'coding'           THEN 'ship_code'
            WHEN 'planning'         THEN 'manage_team'
            WHEN 'writing_research' THEN 'write_publish'
            WHEN 'explore'          THEN 'evaluate'
            WHEN 'other'            THEN 'other'
            ELSE NULL
        END,
        'use_case_other', onboarding_questionnaire->>'use_case_other',
        'use_case_skipped', false,
        'version', 2
    ))
WHERE onboarding_questionnaire IS NOT NULL
  AND (onboarding_questionnaire ? 'team_size'
       OR onboarding_questionnaire ? 'role'
       OR onboarding_questionnaire ? 'use_case')
  AND (onboarding_questionnaire->>'version' IS NULL
       OR onboarding_questionnaire->>'version' = '1');
