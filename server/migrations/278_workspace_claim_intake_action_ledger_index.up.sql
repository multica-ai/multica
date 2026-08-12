CREATE INDEX CONCURRENTLY workspace_claim_intake_action_ledger_idx ON workspace_claim_intake_action (workspace_id, created_at DESC, id DESC);
