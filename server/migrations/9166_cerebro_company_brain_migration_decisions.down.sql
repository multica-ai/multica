-- CEREBRO-PATCH(company-brain-migration-decisions): FIR-3924 rollback.

DROP TABLE IF EXISTS cerebro_company_brain_migration_decision;

ALTER TABLE IF EXISTS cerebro_company_brain_parity_proof
    DROP CONSTRAINT IF EXISTS cerebro_company_brain_parity_proof_agent_fk;

ALTER TABLE agent
    DROP CONSTRAINT IF EXISTS agent_workspace_id_id_unique;

ALTER TABLE cerebro_company_brain_connection
    DROP CONSTRAINT IF EXISTS cerebro_company_brain_connection_workspace_id_id_unique;
