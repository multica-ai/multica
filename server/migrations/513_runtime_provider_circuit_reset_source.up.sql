-- Migration 512 (SE-37711 / SE-37664): persist the reset-source classification
-- on the provider circuit so a never-silent dispatch audit can name WHY a hold
-- window was chosen.
--
-- The failure classifier already derives a reset source when it opens a circuit
-- (parseable retry-after, opaque quota interval, or the auth hold window), but
-- until now that fact was only logged and lost. Recording it lets the pool
-- selector surface "hold class + until + reset source" as durable evidence and
-- lets operators distinguish a provider-advertised retry-after from a
-- conservatively-assumed opaque window.
--
-- Nullable TEXT with no default: rows opened before this migration keep a NULL
-- reset_source, which the reader treats as "unknown".
ALTER TABLE runtime_provider_circuit
  ADD COLUMN IF NOT EXISTS reset_source TEXT;
