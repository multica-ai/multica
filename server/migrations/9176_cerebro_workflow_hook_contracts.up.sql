ALTER TABLE cerebro_workflow_hook_policy
    ADD COLUMN IF NOT EXISTS contract_rule TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS contract_satisfy TEXT NOT NULL DEFAULT '';

WITH contracts AS (
    SELECT
        policy.id,
        CASE
            WHEN policy.name LIKE 'Route task failure: %' THEN
                'When a task fails with '
                || substring(policy.name FROM length('Route task failure: ') + 1)
                || ', the platform applies the '
                || COALESCE(NULLIF(handler.modifications->>'failure_action', ''), 'surface')
                || ' action.'
            ELSE COALESCE(NULLIF(btrim(policy.description), ''), policy.name)
        END AS contract_rule,
        CASE
            WHEN policy.name LIKE 'Route task failure: %' THEN
                'No agent action is required; the platform handles this failure automatically.'
            ELSE COALESCE(
                NULLIF(btrim(handler.requirement), ''),
                'The platform handles matching events automatically.'
            )
        END AS contract_satisfy
    FROM cerebro_workflow_hook_policy policy
    LEFT JOIN LATERAL (
        SELECT requirement, modifications
        FROM cerebro_workflow_hook_handler
        WHERE policy_id = policy.id
        ORDER BY position
        LIMIT 1
    ) handler ON TRUE
)
UPDATE cerebro_workflow_hook_policy policy
SET contract_rule = contracts.contract_rule,
    contract_satisfy = contracts.contract_satisfy,
    updated_at = now()
FROM contracts
WHERE contracts.id = policy.id
  AND (btrim(policy.contract_rule) = '' OR btrim(policy.contract_satisfy) = '');

WITH contracts AS (
    SELECT
        id,
        COALESCE(
            NULLIF(btrim(configuration->>'description'), ''),
            NULLIF(btrim(configuration->>'name'), ''),
            'Untitled hook'
        ) AS contract_rule,
        COALESCE(
            NULLIF(btrim(configuration#>>'{handlers,0,requirement}'), ''),
            'The platform handles matching events automatically.'
        ) AS contract_satisfy
    FROM cerebro_workflow_hook_draft_revision
)
UPDATE cerebro_workflow_hook_draft_revision revision
SET configuration = jsonb_set(
        jsonb_set(revision.configuration, '{contract_rule}', to_jsonb(contracts.contract_rule)),
        '{contract_satisfy}', to_jsonb(contracts.contract_satisfy)
    ),
    configuration_hash = encode(digest(
        jsonb_set(
            jsonb_set(revision.configuration, '{contract_rule}', to_jsonb(contracts.contract_rule)),
            '{contract_satisfy}', to_jsonb(contracts.contract_satisfy)
        )::text,
        'sha256'
    ), 'hex'),
    updated_at = now()
FROM contracts
WHERE contracts.id = revision.id
  AND (
      btrim(COALESCE(revision.configuration->>'contract_rule', '')) = ''
      OR btrim(COALESCE(revision.configuration->>'contract_satisfy', '')) = ''
  );
