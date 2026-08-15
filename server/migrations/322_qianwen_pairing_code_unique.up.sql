-- Single statement: CREATE UNIQUE INDEX CONCURRENTLY cannot run inside a
-- transaction. This is also the ON CONFLICT arbiter that atomically replaces
-- an older code minted for the same installation and target user.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_qianwen_pairing_code_installation_user
    ON qianwen_pairing_code (installation_id, multica_user_id);
