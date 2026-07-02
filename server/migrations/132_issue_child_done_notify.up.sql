-- Per-parent opt-out for the child-done -> parent notification + wake.
-- When false, a completing sub-issue neither posts the system comment on the
-- parent nor wakes the parent's assignee (see issue_child_done.go). Default
-- true preserves the existing production behavior for all current parents.
-- Only meaningful for agent/squad-assigned parents; human-assigned parents are
-- already skipped by the member-assignee guard.
ALTER TABLE issue ADD COLUMN child_done_notify BOOLEAN NOT NULL DEFAULT true;
