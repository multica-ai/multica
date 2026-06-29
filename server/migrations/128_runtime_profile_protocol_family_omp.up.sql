-- Add 'omp' (Oh My Pi, ACP backend) to the runtime_profile.protocol_family
-- allow-list. See MUL omp-backend rollout.
--
-- protocol_family must stay in lockstep with the agent.New() switch /
-- SupportedTypes in server/pkg/agent/agent.go, which now includes 'omp'. A
-- profile may only be based on a backend Multica officially supports and tests.
--
-- Migration 126 (drop_gemini) re-declared this CHECK as a NOT VALID table
-- constraint with 'gemini' removed. There is no ALTER ... ALTER CONSTRAINT for
-- CHECK predicates, so we drop and re-add it with 'omp' appended. We preserve
-- 126's post-removal list (no 'gemini') and its NOT VALID semantics — enforce
-- the whitelist for future writes without validating historical rows — so this
-- migration does not silently re-introduce 'gemini' or block upgrades for
-- workspaces with legacy profiles.

ALTER TABLE runtime_profile DROP CONSTRAINT IF EXISTS runtime_profile_protocol_family_check;

ALTER TABLE runtime_profile ADD CONSTRAINT runtime_profile_protocol_family_check
    CHECK (protocol_family IN (
        'claude',
        'codebuddy',
        'codex',
        'copilot',
        'opencode',
        'openclaw',
        'hermes',
        'pi',
        'cursor',
        'kimi',
        'kiro',
        'antigravity',
        'omp'
    )) NOT VALID;
