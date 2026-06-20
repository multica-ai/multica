-- 9089_cerebro_tool_policy_conditions: optional Condition (the WHEN layer).
--
-- FIR-1609. A rule may carry an optional Condition that refines whether it
-- applies to a request (host allowlist, action set, or a CEL expression). The
-- Condition never decides Allow/Ask/Deny — that stays the Effect's job. Stored
-- as nullable JSONB matching toolpolicy.Condition; NULL means "no condition,
-- always applies", so every existing row is unaffected. Precompiled / evaluated
-- off the hot path, keeping chain resolution order-independent.

ALTER TABLE cerebro_tool_policy
    ADD COLUMN IF NOT EXISTS conditions JSONB;
