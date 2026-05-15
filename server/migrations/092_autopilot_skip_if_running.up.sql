-- Add a narrow, enforceable overlap policy for autopilot dispatch.
-- This intentionally does not restore the removed concurrency_policy enum:
-- queue/replace need separate scheduler/cancellation semantics before they can
-- be exposed honestly.
ALTER TABLE autopilot
ADD COLUMN IF NOT EXISTS skip_if_running BOOLEAN NOT NULL DEFAULT true;
