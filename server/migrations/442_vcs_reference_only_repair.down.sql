-- reference_only is part of the base VCS schema from migration 216. Rolling
-- back this repair must not remove a column that healthy installations already
-- had before migration 442.
SELECT 1;
