-- TECH-3487: capture the conversation a self-scheduled wakeup was created from,
-- so when the wakeup fires the agent's reply lands back in the ORIGINAL thread
-- instead of in a new orphaned root note. origin_comment_id is the thread anchor
-- (root comment) of the thread the scheduling agent was in. NULL when the wakeup
-- was created outside a comment thread (e.g. a fresh assignment) — dispatch then
-- falls back to posting the wakeup note as a root comment (pre-TECH-3487 behavior).
ALTER TABLE cerebro_agent_wakeup
    ADD COLUMN IF NOT EXISTS origin_comment_id uuid REFERENCES comment(id) ON DELETE SET NULL;
