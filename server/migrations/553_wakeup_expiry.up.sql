-- Every wakeup can carry an end: an absolute deadline, or a relative wait that
-- restarts from "now" whenever the rule is (re)enabled. on_timeout decides
-- whether reaching the deadline wakes the target once to handle it or simply
-- ends the rule. timed_out_at records that the deadline, not a trigger or a
-- person, ended the rule. All columns are nullable: existing rules keep their
-- previous open-ended behavior.
ALTER TABLE issue_wakeup
 ADD COLUMN IF NOT EXISTS expires_at timestamptz,
 ADD COLUMN IF NOT EXISTS expiry_seconds bigint,
 ADD COLUMN IF NOT EXISTS on_timeout text CHECK (on_timeout IN ('wake','end')),
 ADD COLUMN IF NOT EXISTS timed_out_at timestamptz;
