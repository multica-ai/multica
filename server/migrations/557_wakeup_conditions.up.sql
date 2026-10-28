-- Platform-evaluated conditions and runaway protection for issue wakeups.
--
-- condition holds a structured predicate (a field value, sub-issues finishing,
-- a linked pull request, another issue's status). Such a rule stays
-- kind='event'; the scheduler evaluates it and only wakes the target when the
-- predicate becomes true. condition_state remembers the last satisfied
-- fingerprint so a rule fires on the change, not on every evaluation.
--
-- max_fires/fire_count cap repeating rules; paused_reason records why the
-- platform, not a person, stopped a rule (the cap, a detected loop, or a
-- burst of triggers). All columns are nullable or defaulted: existing rules
-- behave as before.
ALTER TABLE issue_wakeup
 ADD COLUMN IF NOT EXISTS condition jsonb,
 ADD COLUMN IF NOT EXISTS condition_state text NOT NULL DEFAULT '',
 ADD COLUMN IF NOT EXISTS max_fires integer CHECK (max_fires BETWEEN 1 AND 1000),
 ADD COLUMN IF NOT EXISTS fire_count integer NOT NULL DEFAULT 0,
 ADD COLUMN IF NOT EXISTS paused_reason text CHECK (paused_reason IN ('max_fires','loop','rate'));
