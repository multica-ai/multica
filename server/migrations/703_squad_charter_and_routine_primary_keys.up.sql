-- Attach the CONCURRENTLY-built unique indexes as the primary keys. One file, so
-- both attaches run in the same implicit transaction and a retry never sees one
-- key attached and the other not.
ALTER TABLE squad_charter ADD CONSTRAINT squad_charter_pkey PRIMARY KEY USING INDEX squad_charter_pkey_uidx;
ALTER TABLE squad_routine ADD CONSTRAINT squad_routine_pkey PRIMARY KEY USING INDEX squad_routine_pkey_uidx;
