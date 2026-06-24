-- FIR-2016 Fase 1: drop two verified-dead permission-adjacent tables.
--
-- cerebro_share_token (9020): public-link share tokens. The sharetoken handler
-- package and its only auth (Persona) were removed; the 5 query funcs have zero
-- live callers. cerebro_agent_infisical_secret (9038): per-agent Infisical
-- secret binding; never had sqlc query code or any reference. Both are leaf
-- tables (no inbound FK), so dropping is behaviour-preserving.

DROP TABLE IF EXISTS cerebro_share_token;
DROP TABLE IF EXISTS cerebro_agent_infisical_secret;
