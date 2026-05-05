DROP INDEX IF EXISTS idx_memory_always_inject_active;
ALTER TABLE memory_artifact DROP COLUMN IF EXISTS always_inject_at_runtime;
