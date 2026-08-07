-- At most one live scan session per agent. Two concurrent QRs for one agent
-- would race to bind it, and the loser's bot would be created on WeCom's side
-- and then silently owned by nobody.
--
-- Partial on the live states so a settled session never blocks a retry.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_wecom_install_session_agent_pending
    ON wecom_install_session (workspace_id, agent_id)
    WHERE status IN ('creating', 'pending');
