ALTER TABLE lark_installation
    ADD COLUMN notification_event_types TEXT[] NOT NULL DEFAULT ARRAY[
        'issue_assigned',
        'mentioned',
        'task_failed',
        'quick_create_failed',
        'autopilot_paused'
    ]::TEXT[];
