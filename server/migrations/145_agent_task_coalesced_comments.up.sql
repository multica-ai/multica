-- Guarantee at-least-once processing of user comments (MUL-4195).
--
-- Historically a new comment that arrived while an agent already had a
-- queued/dispatched task for the same (issue, agent) was silently DROPPED by
-- the HasPendingTaskForIssueAndAgent dedup: only the first comment's
-- trigger_comment_id survived, and the follow-up instruction was lost with no
-- visible trace. That is a correctness bug for comments, which — unlike chat —
-- are deliberate, addressed, persisted user input that must never vanish.
--
-- coalesced_comment_ids records the comments that were FOLDED INTO a
-- not-yet-started task instead of being dropped. The task's trigger_comment_id
-- is repointed to the newest comment (so the injected prompt shows the latest
-- deliberate instruction) while this array preserves every earlier comment the
-- single run must still address. It is also surfaced on the task API so the UI
-- can show exactly which comments a run covered.
--
-- Default '{}' (empty array, never NULL) so existing rows and every non-merge
-- enqueue path keep working untouched.
ALTER TABLE agent_task_queue
    ADD COLUMN coalesced_comment_ids UUID[] NOT NULL DEFAULT '{}';
