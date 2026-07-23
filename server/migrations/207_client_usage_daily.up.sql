CREATE TABLE client_usage_daily (
    user_id UUID NOT NULL,
    client_type TEXT NOT NULL CHECK (client_type IN ('web', 'desktop')),
    install_id UUID NOT NULL,
    activity_date DATE NOT NULL,
    workspace_id UUID,
    client_version TEXT NOT NULL,
    os TEXT NOT NULL,
    first_active_at TIMESTAMPTZ NOT NULL,
    last_active_at TIMESTAMPTZ NOT NULL,
    runtime_probed_at TIMESTAMPTZ,
    probe_result TEXT CHECK (probe_result IN ('success', 'error')),
    runtime_count INTEGER CHECK (runtime_count >= 0),
    provider_summary JSONB,
    online_count INTEGER CHECK (online_count >= 0),
    offline_count INTEGER CHECK (offline_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (first_active_at <= last_active_at),
    CHECK (
        (runtime_probed_at IS NULL AND probe_result IS NULL AND runtime_count IS NULL AND provider_summary IS NULL AND online_count IS NULL AND offline_count IS NULL)
        OR
        (runtime_probed_at IS NOT NULL AND probe_result = 'error' AND runtime_count IS NULL AND provider_summary IS NULL AND online_count IS NULL AND offline_count IS NULL)
        OR
        (runtime_probed_at IS NOT NULL AND probe_result = 'success' AND runtime_count IS NOT NULL AND provider_summary IS NOT NULL AND online_count IS NOT NULL AND offline_count IS NOT NULL AND online_count + offline_count = runtime_count AND jsonb_typeof(provider_summary) = 'object')
    )
);
