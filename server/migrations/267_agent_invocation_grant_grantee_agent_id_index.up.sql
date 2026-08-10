-- Reverse lookup index for the A2A invocation whitelist (NEX-24): backs
-- "which agents may this grantee invoke" filters and grantee-removal cleanup
-- on agent hard-delete (application layer, Stage 3).
--
-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction or multi-command string (repo convention, e.g. migration 138).
CREATE INDEX CONCURRENTLY IF NOT EXISTS agent_invocation_grant_grantee_agent_id_idx
    ON agent_invocation_grant(grantee_agent_id);
