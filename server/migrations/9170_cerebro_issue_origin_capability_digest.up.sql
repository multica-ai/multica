-- FIR-4012: extend issue.origin_type to allow 'capability_digest'.
--
-- The drift watcher stamps (origin_type='capability_digest', origin_id=<workspace id>)
-- on the workspace capability digest so GetOpenCapabilityDigestIssue dedupes in
-- O(1) (driftwatch/sweeper.go digestOriginType). That value was never added to
-- the CHECK, so every nightly CreateIssueWithOrigin was rejected with
-- `violates check constraint "issue_origin_type_check"` — the sweep found its
-- 52 unpermitted capabilities and then silently failed to write the digest, and
-- burnt one issue number per night on the counter it had already incremented.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'runtime_approval', 'agent_task', 'lark_chat', 'note_comment', 'capability_digest'));
