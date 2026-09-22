-- Dropping the circuit loses only in-flight provider holds; they rebuild from
-- the next terminal failure. The runtime pools themselves are unaffected.
DROP TABLE IF EXISTS runtime_provider_circuit;
