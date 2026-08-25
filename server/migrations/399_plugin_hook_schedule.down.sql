DELETE FROM plugin_invocation WHERE trigger = 'schedule';

ALTER TABLE plugin_invocation DROP CONSTRAINT IF EXISTS plugin_invocation_trigger_check;
ALTER TABLE plugin_invocation ADD CONSTRAINT plugin_invocation_trigger_check
    CHECK (trigger IN ('ui', 'manual', 'event', 'agent')) NOT VALID;
ALTER TABLE plugin_invocation DROP COLUMN IF EXISTS planned_at;
ALTER TABLE plugin_invocation DROP COLUMN IF EXISTS delivery_id;

DROP TABLE IF EXISTS plugin_hook_schedule;
