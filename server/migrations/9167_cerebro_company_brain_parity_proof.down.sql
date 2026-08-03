-- CEREBRO-PATCH(company-brain-parity-proof): FIR-3924 rollback.

DROP TABLE IF EXISTS cerebro_company_brain_parity_proof;

ALTER TABLE cerebro_tool_policy
    DROP CONSTRAINT IF EXISTS cerebro_tool_policy_company_brain_parity_identity_unique;
