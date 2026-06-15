-- Revert the default-interactive backfill: put daemon runtimes back to
-- headless. This is best-effort — it cannot distinguish a row that was
-- interactive before the up migration from one it flipped, so a rollback
-- resets every non-cloud runtime to headless.

UPDATE agent_runtime
SET presentation_mode = 'headless', updated_at = now()
WHERE presentation_mode = 'interactive'
  AND provider <> 'firtal-gateway';
