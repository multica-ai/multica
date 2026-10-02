-- instruction_by records who set a system rule's instruction on its issue. The
-- instruction reaches the agent the rule wakes, so it applies only while that
-- person may use the agent; without a recorded person the run gets the
-- default instruction. Nullable and additive: existing rows keep their text
-- and fall back to the default until someone sets it again.
ALTER TABLE issue_wakeup ADD COLUMN IF NOT EXISTS instruction_by uuid;
