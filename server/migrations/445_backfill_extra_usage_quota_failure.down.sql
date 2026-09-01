-- Intentionally irreversible. Once the up migration has run, a row it repaired
-- is indistinguishable from a correctly classified row written by the newer
-- application. Reverting every matching quota row would corrupt those organic
-- writes, while preserving the refined TEXT value is safe for older binaries.
SELECT 1;
