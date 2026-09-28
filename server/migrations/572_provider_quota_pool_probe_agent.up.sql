ALTER TABLE provider_quota_pool ADD COLUMN probe_agent_id uuid;
ALTER TABLE provider_quota_pool_event DROP CONSTRAINT provider_quota_pool_event_event_type_check;
ALTER TABLE provider_quota_pool_event ADD CONSTRAINT provider_quota_pool_event_event_type_check
    CHECK (event_type IN ('held', 'extended', 'probe_due', 'probe_started', 'probe_succeeded', 'probe_failed', 'released', 'membership_changed', 'probe_agent_changed'));
