-- Leave pg_trgm installed. Other queries or operator-created indexes may depend
-- on it, and an idle extension has no meaningful overhead.
SELECT 1;
