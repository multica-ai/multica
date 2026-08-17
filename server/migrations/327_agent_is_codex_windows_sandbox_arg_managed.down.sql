-- Restore the pre-migration data shape before provenance is discarded.
-- Only rows whose ownership bit proves Multica injected the exact canonical
-- leading pair are rewritten. Removing index zero twice preserves every suffix
-- element and removes exactly one pair when duplicate canonical pairs exist.
UPDATE agent
SET custom_args = (custom_args - 0) - 0
WHERE is_codex_windows_sandbox_arg_managed IS TRUE
  AND jsonb_typeof(custom_args) = 'array'
  AND custom_args ->> 0 = '-c'
  AND custom_args ->> 1 = 'windows.sandbox="unelevated"';

ALTER TABLE agent
    DROP COLUMN is_codex_windows_sandbox_arg_managed;

