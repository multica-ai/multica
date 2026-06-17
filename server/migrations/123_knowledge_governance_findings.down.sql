DELETE FROM knowledge_source WHERE source_type = 'knowledge';

ALTER TABLE knowledge_source
    DROP CONSTRAINT IF EXISTS knowledge_source_source_type_check;

ALTER TABLE knowledge_source
    ADD CONSTRAINT knowledge_source_source_type_check
    CHECK (source_type IN ('issue', 'comment', 'agent_task', 'pull_request', 'commit'));

DROP TABLE IF EXISTS knowledge_governance_finding;
