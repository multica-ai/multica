ALTER TABLE agent_runtime
    ADD COLUMN claim_window_start TIME WITHOUT TIME ZONE,
    ADD COLUMN claim_window_timezone TEXT,
    ADD CONSTRAINT agent_runtime_claim_window_pair
        CHECK (
            (claim_window_start IS NULL AND claim_window_timezone IS NULL)
            OR
            (claim_window_start IS NOT NULL AND claim_window_timezone IS NOT NULL)
        );
