ALTER TABLE agent
    ADD COLUMN operating_mode TEXT NOT NULL DEFAULT 'coding'
    CONSTRAINT agent_operating_mode_check
    CHECK (operating_mode IN ('coding', 'operational', 'hybrid'));
