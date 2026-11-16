-- Project start_date / due_date make a Project a schedulable planning object
-- alongside its issues: a planned start plus a deadline. These mirror
-- issue.start_date / issue.due_date after migration 112 — calendar days stored
-- as DATE, carrying no time-of-day or timezone, so "Mar 1" means Mar 1 for
-- every viewer regardless of their local offset (see GH #3618 / MUL-2925).
-- Backs MUL-4388 / GH #5227. Nullable, no default: adding a nullable column is
-- a metadata-only change with no table rewrite.
ALTER TABLE project
    ADD COLUMN start_date DATE,
    ADD COLUMN due_date DATE;
