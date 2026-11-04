-- The daily floor brief routine of a floor's chief (spec 4.8). The chief's
-- autopilot is recorded on its floor_chief row, so creating the routine is
-- idempotent: one routine per chief, found again without a title match. Nullable,
-- and no foreign key: the autopilot is validated in application code.
ALTER TABLE floor_chief ADD COLUMN IF NOT EXISTS brief_autopilot_id UUID;
