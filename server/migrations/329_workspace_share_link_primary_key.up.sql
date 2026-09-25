-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE workspace_share_link
    ADD CONSTRAINT workspace_share_link_pkey PRIMARY KEY USING INDEX workspace_share_link_pkey_uidx;
