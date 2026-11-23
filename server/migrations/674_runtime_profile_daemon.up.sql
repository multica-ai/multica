-- NULL identifies existing profiles; only their existing daemons may refresh them.
-- Do not infer a creating machine from replicated runtime rows.
ALTER TABLE runtime_profile ADD COLUMN IF NOT EXISTS daemon_id TEXT;
