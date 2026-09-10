ALTER TABLE issue_workflow
    ADD CONSTRAINT issue_workflow_pkey PRIMARY KEY USING INDEX issue_workflow_pkey_uidx;
ALTER TABLE issue_workflow_status
    ADD CONSTRAINT issue_workflow_status_pkey PRIMARY KEY USING INDEX issue_workflow_status_pkey_uidx;
ALTER TABLE issue_transition
    ADD CONSTRAINT issue_transition_pkey PRIMARY KEY USING INDEX issue_transition_pkey_uidx;
ALTER TABLE automation_execution
    ADD CONSTRAINT automation_execution_pkey PRIMARY KEY USING INDEX automation_execution_pkey_uidx;
