-- Agent profile v2 (JEH-1031).
-- Replaces the single 0-100 tech_pref slider with 4 separate 1-5 scope
-- questions (git / code / computer use / process) and adds an escape hatch
-- for users who want to write their own prompt — either appended to or
-- replacing the compiled output.
--
-- Pre-launch — no production data exists, so the old column is dropped
-- outright rather than backfilled.

ALTER TABLE user_profile
    DROP COLUMN IF EXISTS tech_pref,
    ADD COLUMN IF NOT EXISTS git_pref SMALLINT NOT NULL DEFAULT 3
        CHECK (git_pref BETWEEN 1 AND 5),
    ADD COLUMN IF NOT EXISTS code_pref SMALLINT NOT NULL DEFAULT 3
        CHECK (code_pref BETWEEN 1 AND 5),
    ADD COLUMN IF NOT EXISTS computer_pref SMALLINT NOT NULL DEFAULT 3
        CHECK (computer_pref BETWEEN 1 AND 5),
    ADD COLUMN IF NOT EXISTS process_pref SMALLINT NOT NULL DEFAULT 3
        CHECK (process_pref BETWEEN 1 AND 5),
    ADD COLUMN IF NOT EXISTS custom_prompt TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prompt_mode TEXT NOT NULL DEFAULT 'append'
        CHECK (prompt_mode IN ('append', 'replace'));
