CREATE TABLE IF NOT EXISTS cerebro_round_cycle (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    round_id UUID NOT NULL REFERENCES cerebro_round(id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS cerebro_round_one_active_cycle
    ON cerebro_round_cycle(round_id) WHERE ended_at IS NULL;

CREATE TABLE IF NOT EXISTS cerebro_round_cycle_item (
    cycle_id UUID NOT NULL REFERENCES cerebro_round_cycle(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    handled_at TIMESTAMPTZ,
    PRIMARY KEY (cycle_id, issue_id)
);

DROP TABLE IF EXISTS cerebro_round_run_item;
DROP TABLE IF EXISTS cerebro_round_run;
DROP TABLE IF EXISTS cerebro_round_held_trigger;

ALTER TABLE cerebro_round
    DROP COLUMN IF EXISTS mode,
    DROP COLUMN IF EXISTS schedule_cron,
    DROP COLUMN IF EXISTS timezone,
    DROP COLUMN IF EXISTS next_run_at,
    DROP COLUMN IF EXISTS cycle_opened_at;
