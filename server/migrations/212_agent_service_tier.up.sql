-- Per-agent Codex service-tier override. The value is the runtime-native
-- catalog ID reported by `codex debug models --bundled` (for example,
-- "priority", displayed by Codex as "Fast"). NULL means "inherit the local
-- Codex configuration / account default" and produces no RPC override.
ALTER TABLE agent ADD COLUMN service_tier TEXT;
