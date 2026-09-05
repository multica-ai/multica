ALTER TABLE issue_lifecycle
    ADD CONSTRAINT issue_lifecycle_pkey PRIMARY KEY USING INDEX issue_lifecycle_pkey_uidx;
ALTER TABLE issue_lifecycle_status
    ADD CONSTRAINT issue_lifecycle_status_pkey PRIMARY KEY USING INDEX issue_lifecycle_status_pkey_uidx;
ALTER TABLE issue_transition
    ADD CONSTRAINT issue_transition_pkey PRIMARY KEY USING INDEX issue_transition_pkey_uidx;
ALTER TABLE automation_execution
    ADD CONSTRAINT automation_execution_pkey PRIMARY KEY USING INDEX automation_execution_pkey_uidx;
