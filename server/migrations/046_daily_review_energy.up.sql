ALTER TABLE daily_review
    ADD COLUMN energy_level INT CHECK (energy_level BETWEEN 1 AND 5),
    ADD COLUMN energy_note TEXT,
    ADD COLUMN recovery_need BOOLEAN;
