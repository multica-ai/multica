-- Unique index backing provider_plan_capacity's primary key and capacity
-- upserts. Kept in its own single-statement migration so the build is
-- non-blocking on an existing self-host table.
CREATE UNIQUE INDEX CONCURRENTLY provider_plan_capacity_owner_provider_uidx
    ON provider_plan_capacity (owner_id, provider);
