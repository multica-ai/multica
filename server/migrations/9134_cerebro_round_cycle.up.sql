-- FIR-3114 review round 3 — a Round surfaces agent responses only inside an
-- explicit answer cycle. cycle_opened_at is set when the owner (or schedule)
-- starts the round and cleared when everything surfaced has been answered
-- (round done) or the owner pauses. Responses arriving while the cycle is
-- closed queue silently for the next start.
ALTER TABLE cerebro_round ADD COLUMN IF NOT EXISTS cycle_opened_at TIMESTAMPTZ;
