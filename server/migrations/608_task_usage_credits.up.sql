-- Provider-reported credits for a usage record.
--
-- Some providers bill in their own credit unit and expose no token counts at
-- all: Qoder Cloud Agents reports `session.usage.total_credits` and
-- `span.model_request_end.model_usage.credits`, nothing else. Converting those
-- to USD would need an exchange rate the provider does not publish, so the
-- figure is stored as-is next to the token counters and shown in its own unit.
--
-- UNIT: millionths of a credit, so fractional credits stay exact integers.
-- NULL means "the provider reported no credits" — every pre-existing row and
-- every token-metered provider.
ALTER TABLE task_usage
    ADD COLUMN IF NOT EXISTS credits_micros BIGINT;
