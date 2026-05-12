ALTER TABLE user_profile
    DROP COLUMN IF EXISTS prompt_mode,
    DROP COLUMN IF EXISTS custom_prompt,
    DROP COLUMN IF EXISTS process_pref,
    DROP COLUMN IF EXISTS computer_pref,
    DROP COLUMN IF EXISTS code_pref,
    DROP COLUMN IF EXISTS git_pref,
    ADD COLUMN IF NOT EXISTS tech_pref SMALLINT NOT NULL DEFAULT 50
        CHECK (tech_pref BETWEEN 0 AND 100);
