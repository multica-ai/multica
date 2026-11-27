ALTER TABLE issue_wakeup
 ADD COLUMN filter_actor_type text,
 ADD COLUMN filter_actor_id uuid,
 ADD CONSTRAINT issue_wakeup_actor_filter CHECK (
  (filter_actor_type IS NULL AND filter_actor_id IS NULL) OR
  (filter_actor_type IS NOT NULL AND filter_actor_type IN ('member','agent') AND filter_actor_id IS NOT NULL AND kind='event')
 ) NOT VALID;
