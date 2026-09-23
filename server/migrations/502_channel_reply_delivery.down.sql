-- Keep delivery ownership on application rollback: dropping it would let an
-- older binary re-send replies the provider has already accepted.
SELECT 1;
