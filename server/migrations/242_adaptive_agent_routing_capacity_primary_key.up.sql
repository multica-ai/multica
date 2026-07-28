ALTER TABLE provider_plan_capacity
    ADD CONSTRAINT provider_plan_capacity_pkey
    PRIMARY KEY USING INDEX provider_plan_capacity_owner_provider_uidx;
