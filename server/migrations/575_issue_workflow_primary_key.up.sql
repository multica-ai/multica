-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE issue_workflow
    ADD CONSTRAINT issue_workflow_pkey PRIMARY KEY USING INDEX issue_workflow_pkey_uidx;
