-- Keep the column: an older binary ignores it, and dropping it would lose the
-- ordering that stops a superseded attempt from rewriting a live reply.
SELECT 1;
