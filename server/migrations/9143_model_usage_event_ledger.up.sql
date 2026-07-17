-- CEREBRO-PATCH(model-usage-event-ledger): FIR-3337 establishes one append-only
-- source for call-level tokens, context occupancy, compactions, and exact cost.
CREATE TABLE IF NOT EXISTS model_usage_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    schema_version TEXT NOT NULL DEFAULT '1',
    event_id TEXT NOT NULL,

    -- Immutable attribution copied from the owning task when the event lands.
    task_id UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    runtime_id UUID REFERENCES agent_runtime(id) ON DELETE SET NULL,
    session_root_comment_id UUID REFERENCES comment(id) ON DELETE SET NULL,
    chat_session_id UUID REFERENCES chat_session(id) ON DELETE SET NULL,
    autopilot_run_id UUID REFERENCES autopilot_run(id) ON DELETE SET NULL,
    parent_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,

    -- Provider-native identity keeps parallel agents and calls separable.
    provider_session_id TEXT,
    call_id TEXT,
    sequence BIGINT NOT NULL DEFAULT 0 CHECK (sequence >= 0),
    observed_at TIMESTAMPTZ NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,

    input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    reasoning_tokens BIGINT NOT NULL DEFAULT 0 CHECK (reasoning_tokens >= 0),
    cache_read_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
    cache_write_tokens BIGINT NOT NULL DEFAULT 0 CHECK (cache_write_tokens >= 0),
    cost_cents BIGINT NOT NULL DEFAULT 0 CHECK (cost_cents >= 0),
    context_tokens BIGINT NOT NULL DEFAULT 0 CHECK (context_tokens >= 0),
    context_window_tokens BIGINT NOT NULL DEFAULT 0 CHECK (context_window_tokens >= 0),
    compaction_kind TEXT NOT NULL DEFAULT ''
        CHECK (compaction_kind IN ('', 'provider_explicit', 'inferred_drop')),
    source TEXT NOT NULL
        CHECK (source IN ('stream', 'final_response', 'transcript_fallback', 'reconciliation')),
    completeness TEXT NOT NULL
        CHECK (completeness IN ('complete', 'tokens_only', 'context_only', 'estimated')),
    counter_semantics TEXT NOT NULL
        CHECK (counter_semantics IN ('delta', 'cumulative')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (context_window_tokens = 0 OR context_tokens <= context_window_tokens),
    UNIQUE (task_id, event_id)
);

CREATE INDEX IF NOT EXISTS idx_model_usage_event_workspace_observed
    ON model_usage_event (workspace_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_usage_event_issue_observed
    ON model_usage_event (issue_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_usage_event_agent_observed
    ON model_usage_event (agent_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS idx_model_usage_event_task_sequence
    ON model_usage_event (task_id, sequence, observed_at);
CREATE INDEX IF NOT EXISTS idx_model_usage_event_session_observed
    ON model_usage_event (session_root_comment_id, observed_at DESC)
    WHERE session_root_comment_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_model_usage_event_provider_session
    ON model_usage_event (provider, provider_session_id, observed_at)
    WHERE provider_session_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_model_usage_event_call_identity
    ON model_usage_event (task_id, provider, COALESCE(provider_session_id, ''), call_id)
    WHERE call_id IS NOT NULL;
