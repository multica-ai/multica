ALTER TABLE instance_telemetry_state
    ADD CONSTRAINT instance_telemetry_state_pkey PRIMARY KEY USING INDEX instance_telemetry_state_singleton_uidx;
