-- Attach the CONCURRENTLY-built unique index as the table's primary key.
ALTER TABLE office_floor ADD CONSTRAINT office_floor_pkey PRIMARY KEY USING INDEX office_floor_pkey_uidx;
