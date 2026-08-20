ALTER TABLE channel_installation
    ADD COLUMN target_type TEXT NOT NULL DEFAULT 'agent'
        CHECK (target_type IN ('agent', 'squad')),
    ADD COLUMN target_id UUID;

UPDATE channel_installation
SET target_id = agent_id
WHERE target_id IS NULL;

ALTER TABLE dingtalk_group_route
    ADD COLUMN target_type TEXT NOT NULL DEFAULT 'agent'
        CHECK (target_type IN ('agent', 'squad')),
    ADD COLUMN target_id UUID;

UPDATE dingtalk_group_route
SET target_id = agent_id
WHERE target_id IS NULL;

ALTER TABLE dingtalk_group_route
    ALTER COLUMN target_id SET NOT NULL;
