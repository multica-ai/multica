-- Cerebro feature (FIR-2698): single-source model registry for LLM model
-- metadata — display label, provider, context window, and list prices — with
-- the same propose → review → approve versioning pattern as skills (9013) and
-- agent context (9100).
--
-- Replaces four hand-maintained in-code tables that had to be kept in sync by
-- convention (the FIR-2689 root cause):
--   server/pkg/pricing/pricing.go            (cost_cents, cents/Mtok)
--   server/internal/metrics/pricing.go       (Prometheus, USD/Mtok)
--   server/internal/cerebro/sessions/context_window.go (windows)
--   packages/views/runtimes/utils.ts         (frontend, USD/Mtok)
-- The daemon-side picker catalog (server/pkg/agent/models.go) intentionally
-- stays in code: it runs on runtime machines with no database access.
--
-- The registry is a deployment-wide singleton (provider list prices are not
-- workspace data). The snapshot document is one JSONB composite, exactly like
-- agent_context_version.snapshot, so the whole table is versioned and diffed
-- as one unit:
--   { "fallback_model": "...",
--     "models": { "<id>": { "label", "provider", "context_window",
--                           "input_usd_per_mtok", "output_usd_per_mtok",
--                           "cache_read_usd_per_mtok", "cache_write_usd_per_mtok" } } }
-- Prices are USD per million tokens (the human-readable unit); the Go pricing
-- shim converts to cents at load time. context_window = 0 means "not curated"
-- and consumers apply their conservative default.

CREATE TABLE IF NOT EXISTS model_registry (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Singleton guard: exactly one live registry row per deployment.
    registry_key TEXT NOT NULL UNIQUE DEFAULT 'default',
    owner_id UUID,
    approver_ids UUID[] NOT NULL DEFAULT '{}',
    current_version TEXT NOT NULL DEFAULT '1.0.0',
    snapshot JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Append-only snapshots of the whole registry at each merge.
CREATE TABLE IF NOT EXISTS model_registry_version (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    registry_id UUID NOT NULL REFERENCES model_registry(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    snapshot JSONB NOT NULL DEFAULT '{}',
    description TEXT NOT NULL DEFAULT '',
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (registry_id, version)
);

CREATE INDEX IF NOT EXISTS idx_model_registry_version_registry
    ON model_registry_version(registry_id);

-- A proposed edit to the registry, not yet merged. proposed_by is polymorphic
-- (user or agent id, no FK) to match agent_change_request.
CREATE TABLE IF NOT EXISTS model_registry_change_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    registry_id UUID NOT NULL REFERENCES model_registry(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    base_version TEXT NOT NULL,
    proposed_version TEXT NOT NULL,
    proposed_snapshot JSONB NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected', 'merged')),
    proposed_by UUID NOT NULL,
    reviewed_by UUID,
    reviewed_at TIMESTAMPTZ,
    review_comment TEXT NOT NULL DEFAULT '',
    work_session_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_model_registry_change_request_registry
    ON model_registry_change_request(registry_id);
CREATE INDEX IF NOT EXISTS idx_model_registry_change_request_status
    ON model_registry_change_request(status)
    WHERE status = 'pending';

-- Seed: the union of every model priced or curated anywhere in the codebase
-- at migration time, at the exact values production billed with. Source
-- priority on conflicts: pkg/pricing (drives cost_cents billing) > utils.ts
-- (drives user-visible dashboards) > internal/metrics (Prometheus only).
-- Context windows only where sessions/context_window.go curated them —
-- everything else is 0 (= consumer default) so fullness indicators keep
-- their pre-registry behavior.
INSERT INTO model_registry (registry_key, current_version, snapshot)
VALUES ('default', '1.0.0', '{
  "fallback_model": "claude-opus-4-1",
  "models": {
    "claude-fable-5":     {"label": "Claude Fable 5",   "provider": "anthropic", "context_window": 1000000, "input_usd_per_mtok": 10,   "output_usd_per_mtok": 50,   "cache_read_usd_per_mtok": 1,      "cache_write_usd_per_mtok": 12.5},
    "claude-opus-4-8":    {"label": "Claude Opus 4.8",  "provider": "anthropic", "context_window": 1000000, "input_usd_per_mtok": 5,    "output_usd_per_mtok": 25,   "cache_read_usd_per_mtok": 0.5,    "cache_write_usd_per_mtok": 6.25},
    "claude-opus-4-7":    {"label": "Claude Opus 4.7",  "provider": "anthropic", "context_window": 1000000, "input_usd_per_mtok": 5,    "output_usd_per_mtok": 25,   "cache_read_usd_per_mtok": 0.5,    "cache_write_usd_per_mtok": 6.25},
    "claude-opus-4-6":    {"label": "Claude Opus 4.6",  "provider": "anthropic", "context_window": 1000000, "input_usd_per_mtok": 5,    "output_usd_per_mtok": 25,   "cache_read_usd_per_mtok": 0.5,    "cache_write_usd_per_mtok": 6.25},
    "claude-opus-4-5":    {"label": "Claude Opus 4.5",  "provider": "anthropic", "context_window": 200000,  "input_usd_per_mtok": 5,    "output_usd_per_mtok": 25,   "cache_read_usd_per_mtok": 0.5,    "cache_write_usd_per_mtok": 6.25},
    "claude-opus-4-1":    {"label": "Claude Opus 4.1",  "provider": "anthropic", "context_window": 200000,  "input_usd_per_mtok": 15,   "output_usd_per_mtok": 75,   "cache_read_usd_per_mtok": 1.5,    "cache_write_usd_per_mtok": 18.75},
    "claude-opus-4":      {"label": "Claude Opus 4",    "provider": "anthropic", "context_window": 200000,  "input_usd_per_mtok": 15,   "output_usd_per_mtok": 75,   "cache_read_usd_per_mtok": 1.5,    "cache_write_usd_per_mtok": 18.75},
    "claude-sonnet-5":    {"label": "Claude Sonnet 5",  "provider": "anthropic", "context_window": 1000000, "input_usd_per_mtok": 3,    "output_usd_per_mtok": 15,   "cache_read_usd_per_mtok": 0.3,    "cache_write_usd_per_mtok": 3.75},
    "claude-sonnet-4-6":  {"label": "Claude Sonnet 4.6","provider": "anthropic", "context_window": 1000000, "input_usd_per_mtok": 3,    "output_usd_per_mtok": 15,   "cache_read_usd_per_mtok": 0.3,    "cache_write_usd_per_mtok": 3.75},
    "claude-sonnet-4-5":  {"label": "Claude Sonnet 4.5","provider": "anthropic", "context_window": 1000000, "input_usd_per_mtok": 3,    "output_usd_per_mtok": 15,   "cache_read_usd_per_mtok": 0.3,    "cache_write_usd_per_mtok": 3.75},
    "claude-sonnet-4":    {"label": "Claude Sonnet 4",  "provider": "anthropic", "context_window": 1000000, "input_usd_per_mtok": 3,    "output_usd_per_mtok": 15,   "cache_read_usd_per_mtok": 0.3,    "cache_write_usd_per_mtok": 3.75},
    "claude-haiku-4-5":   {"label": "Claude Haiku 4.5", "provider": "anthropic", "context_window": 200000,  "input_usd_per_mtok": 1,    "output_usd_per_mtok": 5,    "cache_read_usd_per_mtok": 0.1,    "cache_write_usd_per_mtok": 1.25},
    "claude-haiku-3-5":   {"label": "Claude Haiku 3.5", "provider": "anthropic", "context_window": 200000,  "input_usd_per_mtok": 0.8,  "output_usd_per_mtok": 4,    "cache_read_usd_per_mtok": 0.08,   "cache_write_usd_per_mtok": 1},
    "gpt-5.5":            {"label": "GPT-5.5",          "provider": "openai",    "context_window": 272000,  "input_usd_per_mtok": 5,    "output_usd_per_mtok": 30,   "cache_read_usd_per_mtok": 0.5,    "cache_write_usd_per_mtok": 5},
    "gpt-5.4":            {"label": "GPT-5.4",          "provider": "openai",    "context_window": 0,       "input_usd_per_mtok": 2.5,  "output_usd_per_mtok": 15,   "cache_read_usd_per_mtok": 0.25,   "cache_write_usd_per_mtok": 2.5},
    "gpt-5.4-mini":       {"label": "GPT-5.4 mini",     "provider": "openai",    "context_window": 0,       "input_usd_per_mtok": 0.75, "output_usd_per_mtok": 4.5,  "cache_read_usd_per_mtok": 0.075,  "cache_write_usd_per_mtok": 0.75},
    "gpt-5.3-codex":      {"label": "GPT-5.3 Codex",    "provider": "openai",    "context_window": 0,       "input_usd_per_mtok": 1.75, "output_usd_per_mtok": 14,   "cache_read_usd_per_mtok": 0.175,  "cache_write_usd_per_mtok": 1.75},
    "gpt-5.2-codex":      {"label": "GPT-5.2 Codex",    "provider": "openai",    "context_window": 0,       "input_usd_per_mtok": 1.75, "output_usd_per_mtok": 14,   "cache_read_usd_per_mtok": 0.175,  "cache_write_usd_per_mtok": 0.175},
    "gpt-5-codex":        {"label": "GPT-5 Codex",      "provider": "openai",    "context_window": 0,       "input_usd_per_mtok": 1.25, "output_usd_per_mtok": 10,   "cache_read_usd_per_mtok": 0.125,  "cache_write_usd_per_mtok": 1.25},
    "gpt-5":              {"label": "GPT-5",            "provider": "openai",    "context_window": 272000,  "input_usd_per_mtok": 1.25, "output_usd_per_mtok": 10,   "cache_read_usd_per_mtok": 0.125,  "cache_write_usd_per_mtok": 0},
    "gpt-5-mini":         {"label": "GPT-5 mini",       "provider": "openai",    "context_window": 272000,  "input_usd_per_mtok": 0.25, "output_usd_per_mtok": 2,    "cache_read_usd_per_mtok": 0.025,  "cache_write_usd_per_mtok": 0},
    "gpt-5-nano":         {"label": "GPT-5 nano",       "provider": "openai",    "context_window": 0,       "input_usd_per_mtok": 0.05, "output_usd_per_mtok": 0.4,  "cache_read_usd_per_mtok": 0.005,  "cache_write_usd_per_mtok": 0.05},
    "o3":                 {"label": "o3",               "provider": "openai",    "context_window": 0,       "input_usd_per_mtok": 2,    "output_usd_per_mtok": 8,    "cache_read_usd_per_mtok": 0.5,    "cache_write_usd_per_mtok": 2},
    "o3-mini":            {"label": "o3-mini",          "provider": "openai",    "context_window": 0,       "input_usd_per_mtok": 1.1,  "output_usd_per_mtok": 4.4,  "cache_read_usd_per_mtok": 0.55,   "cache_write_usd_per_mtok": 1.1},
    "o4-mini":            {"label": "o4-mini",          "provider": "openai",    "context_window": 0,       "input_usd_per_mtok": 1.1,  "output_usd_per_mtok": 4.4,  "cache_read_usd_per_mtok": 0.275,  "cache_write_usd_per_mtok": 1.1},
    "gpt-4o":             {"label": "GPT-4o",           "provider": "openai",    "context_window": 0,       "input_usd_per_mtok": 2.5,  "output_usd_per_mtok": 10,   "cache_read_usd_per_mtok": 1.25,   "cache_write_usd_per_mtok": 2.5},
    "gpt-4o-mini":        {"label": "GPT-4o mini",      "provider": "openai",    "context_window": 0,       "input_usd_per_mtok": 0.15, "output_usd_per_mtok": 0.6,  "cache_read_usd_per_mtok": 0.075,  "cache_write_usd_per_mtok": 0.15},
    "gemini-2.5-pro":     {"label": "Gemini 2.5 Pro",   "provider": "google",    "context_window": 1000000, "input_usd_per_mtok": 1.25, "output_usd_per_mtok": 10,   "cache_read_usd_per_mtok": 0.3125, "cache_write_usd_per_mtok": 0},
    "gemini-2.5-flash":   {"label": "Gemini 2.5 Flash", "provider": "google",    "context_window": 1000000, "input_usd_per_mtok": 0.075,"output_usd_per_mtok": 0.3,  "cache_read_usd_per_mtok": 0.01875,"cache_write_usd_per_mtok": 0},
    "gemini-3-flash":     {"label": "Gemini 3 Flash",   "provider": "google",    "context_window": 0,       "input_usd_per_mtok": 0.5,  "output_usd_per_mtok": 3,    "cache_read_usd_per_mtok": 0.05,   "cache_write_usd_per_mtok": 0.5},
    "gemini-3.1-pro":     {"label": "Gemini 3.1 Pro",   "provider": "google",    "context_window": 0,       "input_usd_per_mtok": 2,    "output_usd_per_mtok": 12,   "cache_read_usd_per_mtok": 0.2,    "cache_write_usd_per_mtok": 2},
    "deepseek-v4-pro":    {"label": "DeepSeek V4 Pro",  "provider": "deepseek",  "context_window": 0,       "input_usd_per_mtok": 1.74, "output_usd_per_mtok": 3.48, "cache_read_usd_per_mtok": 0.0145, "cache_write_usd_per_mtok": 1.74},
    "deepseek-v4-flash":  {"label": "DeepSeek V4 Flash","provider": "deepseek",  "context_window": 0,       "input_usd_per_mtok": 0.14, "output_usd_per_mtok": 0.28, "cache_read_usd_per_mtok": 0.0028, "cache_write_usd_per_mtok": 0.14},
    "deepseek-chat":      {"label": "DeepSeek Chat",    "provider": "deepseek",  "context_window": 0,       "input_usd_per_mtok": 0.14, "output_usd_per_mtok": 0.28, "cache_read_usd_per_mtok": 0.0028, "cache_write_usd_per_mtok": 0.14},
    "deepseek-reasoner":  {"label": "DeepSeek Reasoner","provider": "deepseek",  "context_window": 0,       "input_usd_per_mtok": 0.14, "output_usd_per_mtok": 0.28, "cache_read_usd_per_mtok": 0.0028, "cache_write_usd_per_mtok": 0.14},
    "minimax-m2.7":       {"label": "MiniMax M2.7",     "provider": "minimax",   "context_window": 0,       "input_usd_per_mtok": 0.3,  "output_usd_per_mtok": 1.2,  "cache_read_usd_per_mtok": 0.06,   "cache_write_usd_per_mtok": 0.375},
    "minimax-m2.7-highspeed": {"label": "MiniMax M2.7 Highspeed", "provider": "minimax", "context_window": 0, "input_usd_per_mtok": 0.6, "output_usd_per_mtok": 2.4, "cache_read_usd_per_mtok": 0.06,  "cache_write_usd_per_mtok": 0.375},
    "kimi-k2.6":          {"label": "Kimi K2.6",        "provider": "moonshot",  "context_window": 0,       "input_usd_per_mtok": 0.95, "output_usd_per_mtok": 4,    "cache_read_usd_per_mtok": 0.16,   "cache_write_usd_per_mtok": 0.95},
    "glm-5.1":            {"label": "GLM-5.1",          "provider": "zhipu",     "context_window": 0,       "input_usd_per_mtok": 1.4,  "output_usd_per_mtok": 4.4,  "cache_read_usd_per_mtok": 0.26,   "cache_write_usd_per_mtok": 1.4},
    "glm-5":              {"label": "GLM-5",            "provider": "zhipu",     "context_window": 0,       "input_usd_per_mtok": 1,    "output_usd_per_mtok": 3.2,  "cache_read_usd_per_mtok": 0.2,    "cache_write_usd_per_mtok": 1},
    "glm-5-turbo":        {"label": "GLM-5 Turbo",      "provider": "zhipu",     "context_window": 0,       "input_usd_per_mtok": 1.2,  "output_usd_per_mtok": 4,    "cache_read_usd_per_mtok": 0.24,   "cache_write_usd_per_mtok": 1.2},
    "glm-4.7":            {"label": "GLM-4.7",          "provider": "zhipu",     "context_window": 0,       "input_usd_per_mtok": 0.6,  "output_usd_per_mtok": 2.2,  "cache_read_usd_per_mtok": 0.11,   "cache_write_usd_per_mtok": 0.6},
    "glm-4.7-flashx":     {"label": "GLM-4.7 FlashX",   "provider": "zhipu",     "context_window": 0,       "input_usd_per_mtok": 0.07, "output_usd_per_mtok": 0.4,  "cache_read_usd_per_mtok": 0.01,   "cache_write_usd_per_mtok": 0.07},
    "glm-4.7-flash":      {"label": "GLM-4.7 Flash",    "provider": "zhipu",     "context_window": 0,       "input_usd_per_mtok": 0,    "output_usd_per_mtok": 0,    "cache_read_usd_per_mtok": 0,      "cache_write_usd_per_mtok": 0},
    "glm-4.6":            {"label": "GLM-4.6",          "provider": "zhipu",     "context_window": 0,       "input_usd_per_mtok": 0.6,  "output_usd_per_mtok": 2.2,  "cache_read_usd_per_mtok": 0.11,   "cache_write_usd_per_mtok": 0.6},
    "glm-4.5":            {"label": "GLM-4.5",          "provider": "zhipu",     "context_window": 0,       "input_usd_per_mtok": 0.6,  "output_usd_per_mtok": 2.2,  "cache_read_usd_per_mtok": 0.11,   "cache_write_usd_per_mtok": 0.6},
    "glm-4.5-x":          {"label": "GLM-4.5-X",        "provider": "zhipu",     "context_window": 0,       "input_usd_per_mtok": 2.2,  "output_usd_per_mtok": 8.9,  "cache_read_usd_per_mtok": 0.45,   "cache_write_usd_per_mtok": 2.2},
    "glm-4.5-air":        {"label": "GLM-4.5 Air",      "provider": "zhipu",     "context_window": 0,       "input_usd_per_mtok": 0.2,  "output_usd_per_mtok": 1.1,  "cache_read_usd_per_mtok": 0.03,   "cache_write_usd_per_mtok": 0.2},
    "glm-4.5-airx":       {"label": "GLM-4.5 AirX",     "provider": "zhipu",     "context_window": 0,       "input_usd_per_mtok": 1.1,  "output_usd_per_mtok": 4.5,  "cache_read_usd_per_mtok": 0.22,   "cache_write_usd_per_mtok": 1.1},
    "glm-4.5-flash":      {"label": "GLM-4.5 Flash",    "provider": "zhipu",     "context_window": 0,       "input_usd_per_mtok": 0,    "output_usd_per_mtok": 0,    "cache_read_usd_per_mtok": 0,      "cache_write_usd_per_mtok": 0}
  }
}'::jsonb)
ON CONFLICT (registry_key) DO NOTHING;

-- Initial version snapshot so the history view renders and the first change
-- request has a base_version to diff against.
INSERT INTO model_registry_version (registry_id, version, snapshot, description)
SELECT r.id, r.current_version, r.snapshot, 'Initial snapshot (seed from in-code tables)'
FROM model_registry r
WHERE r.registry_key = 'default'
  AND NOT EXISTS (
    SELECT 1 FROM model_registry_version v WHERE v.registry_id = r.id
  );
