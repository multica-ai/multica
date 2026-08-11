ALTER TABLE agent
    ADD COLUMN comment_mention_policy TEXT NOT NULL DEFAULT 'unrestricted';

ALTER TABLE agent
    ADD CONSTRAINT agent_comment_mention_policy_check
    CHECK (comment_mention_policy IN ('unrestricted', 'creator_only_for_non_creator'));

-- Preserve the existing Pool-task behaviour while making the policy an
-- explicit Agent capability that fixed and Pool Agents can both opt into.
UPDATE agent
SET comment_mention_policy = 'creator_only_for_non_creator',
    updated_at = now()
WHERE runtime_binding_mode = 'pool';
