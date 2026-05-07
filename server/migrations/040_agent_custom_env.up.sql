-- CEREBRO-PATCH(migration-idempotent-040-agent-custom-env): cerebro modification of upstream file
-- Add custom_env column to agent table for user-configurable environment
-- variables that get injected into the agent subprocess at launch time.
-- Supports router/proxy (ANTHROPIC_API_KEY, ANTHROPIC_BASE_URL),
-- Bedrock (CLAUDE_CODE_USE_BEDROCK + AWS creds), and Vertex AI modes.
ALTER TABLE agent ADD COLUMN IF NOT EXISTS custom_env JSONB NOT NULL DEFAULT '{}';
