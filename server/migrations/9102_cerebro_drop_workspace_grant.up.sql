-- Retire the cerebro_workspace_grant control plane (FIR-1512).
--
-- The table sat empty and write-dead in production: every grant-authoring
-- surface (agent grant tools, `multica grant` CLI, operator grant control
-- plane) was removed in Spor 1 (#1688) and #1768, and the permission resolver
-- now resolves against an empty grant set (deny-by-default) without reading
-- this table. Dropping it is therefore behaviour-preserving.
--
-- Drop the audit ledger first — it has a FK to cerebro_workspace_grant.

DROP TABLE IF EXISTS cerebro_workspace_grant_audit;
DROP TABLE IF EXISTS cerebro_workspace_grant;
