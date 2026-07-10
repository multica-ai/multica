CREATE INDEX activity_log_space_lifecycle_idx
    ON activity_log (workspace_id, (details->>'space_id'), created_at DESC)
    WHERE action IN (
        'space_archived',
        'space_restored',
        'space_autopilots_resumed'
    );

CREATE INDEX activity_log_integration_space_ids_idx
    ON activity_log USING gin ((details->'affected_space_ids'))
    WHERE action = 'integration_space_bindings_replaced';
