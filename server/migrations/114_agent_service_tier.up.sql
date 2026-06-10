-- Codex-only service tier override. NULL means "do not inject a
-- service_tier override; let the local Codex config/default decide".
-- Supported API values are intentionally limited in the handler to:
--   fast    -> enable Codex Fast mode
--   default -> explicitly use the standard service tier
ALTER TABLE agent ADD COLUMN service_tier TEXT;
