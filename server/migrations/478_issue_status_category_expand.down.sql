-- Forward-only: restoring either strict vocabulary would reject mixed data.
-- Keep compatibility constraints/functions; repair forward and contract only
-- after the separate backfill and verification have completed (MUL-7365).
SELECT 1;
