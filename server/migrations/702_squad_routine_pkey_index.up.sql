-- Backing index for squad_routine's primary key (autopilot_id), attached in 703.
-- Also what makes an autopilot link at most one routine.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS squad_routine_pkey_uidx ON squad_routine (autopilot_id);
