CREATE UNIQUE INDEX CONCURRENTLY execution_ledger_idempotency_uidx ON execution_ledger (idempotency_key);
