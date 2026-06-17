-- No-op: the one-time counter reset is not reversible. The prior dispatch-era
-- counts carry no meaning under the new postpone semantics and are not recoverable.
SELECT 1;
