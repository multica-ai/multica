-- Removing the filter function while rules still reference actors would widen
-- subscriptions. Retain the additive schema on application rollback, or first
-- disable/drain and remove these configurations before schema rollback.
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM issue_wakeup WHERE filter_actor_type IS NOT NULL) THEN
  RAISE EXCEPTION 'Remove actor-filtered wakeups before rolling back actor subscription support';
 END IF;
END $$;
ALTER TABLE issue_wakeup DROP CONSTRAINT issue_wakeup_actor_filter, DROP COLUMN filter_actor_id, DROP COLUMN filter_actor_type;
