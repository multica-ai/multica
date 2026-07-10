-- CEREBRO-PATCH(connection-instructions): FIR-2760 permission-scoped agent guidance for workspace connections.
ALTER TABLE workspace_connection ADD COLUMN IF NOT EXISTS instructions TEXT NOT NULL DEFAULT '';
