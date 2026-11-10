-- Bookkeeping for the "Star us on GitHub" prompt. The prompt shows a bounded
-- number of times with a cooldown between showings, and never again once the
-- user clicks through to GitHub.
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS star_prompt_shown_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS star_prompt_last_shown_at TIMESTAMPTZ;
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS star_prompt_starred_at TIMESTAMPTZ;
