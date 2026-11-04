-- Two phase 4 pieces that need a column and a table (spec 4.2 and 4.7).
--
-- knowledge_link.amount_micros: the amount a budget_request document states,
-- read from its "Amount" section when the document is written (budget_request.go).
-- The plan gate reads it to size the initiative's budget. Empty for every other
-- document and for a budget_request with no readable amount.
ALTER TABLE knowledge_link ADD COLUMN IF NOT EXISTS amount_micros BIGINT;

-- retro_request: a durable follow-up written in the same transaction as an
-- initiative's close (spec 4.7). The retrospective trigger reads it on its next run,
-- so a close is never lost even when the run is not immediate. processed_at is set
-- once the trigger has been evaluated for it. No index: the rows exist only until
-- they are processed, so the table stays small.
CREATE TABLE IF NOT EXISTS retro_request (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    trigger_kind TEXT NOT NULL CHECK (trigger_kind IN ('initiative_closed')),
    subject_id UUID NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ
);
