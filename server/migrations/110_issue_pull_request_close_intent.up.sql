-- Persist whether a PR ↔ issue link was created with explicit closing intent
-- (a "Closes/Fixes/Resolves PREFIX-N" keyword in the PR title or body). The
-- webhook auto-advance gate consults this column so a link-only sibling PR
-- closing later still resolves an issue that an earlier closing-keyword PR
-- had blocked from advancing.
-- CEREBRO-PATCH(migration-idempotent-110-close-intent): add IF NOT EXISTS so the
-- fork's TestMigrationsAreIdempotent second pass does not fail on an already-added
-- column. Upstream ships this as a bare ADD COLUMN; the fork requires idempotency.
ALTER TABLE issue_pull_request
    ADD COLUMN IF NOT EXISTS close_intent BOOLEAN NOT NULL DEFAULT FALSE;
