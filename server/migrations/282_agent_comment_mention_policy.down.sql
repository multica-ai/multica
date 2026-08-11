ALTER TABLE agent
    DROP CONSTRAINT agent_comment_mention_policy_check;

ALTER TABLE agent
    DROP COLUMN comment_mention_policy;
