UPDATE cerebro_workflow_hook_draft_revision
SET configuration = configuration - 'contract_rule' - 'contract_satisfy',
    configuration_hash = encode(digest(
        (configuration - 'contract_rule' - 'contract_satisfy')::text,
        'sha256'
    ), 'hex'),
    updated_at = now();

ALTER TABLE cerebro_workflow_hook_policy
    DROP COLUMN IF EXISTS contract_satisfy,
    DROP COLUMN IF EXISTS contract_rule;
