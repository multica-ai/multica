-- 9045_cerebro_cost_optimization_holdout: random-holdout A/B columns for the
-- cost-saving measurement table (FIR-2325 phase 5).
--
-- Phase 4 measured what each saving WOULD have saved (shadow) or what it saved
-- against a per-run hypothetical baseline (on). That hypothetical is the
-- "estimated would-save" number. Phase 5 adds the trustworthy "measured
-- actually-saved" number via a random holdout: when a saving is "on", a random
-- fraction of runs deliberately run WITHOUT the saving as a control group, and
-- the dashboard compares the control group's real cost against the treatment
-- group's real cost.
--
-- Two columns support that comparison:
--   held_out         — true when the run is the control arm of an "on" saving:
--                       the saving was deliberately withheld this run so it can
--                       be compared against runs where it was applied. Shadow
--                       rows are never held out (they already run unchanged).
--   actual_cost_cents — the run's real billed model cost (US cents), stored on
--                       every measurement row regardless of saving so the
--                       dashboard can average treatment vs. control actual cost.
--
-- Existing rows predate the holdout and are treated as treatment (held_out
-- false) with an unknown actual cost (0).

ALTER TABLE cerebro_cost_optimization_measurement
    ADD COLUMN IF NOT EXISTS held_out BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS actual_cost_cents BIGINT NOT NULL DEFAULT 0;
