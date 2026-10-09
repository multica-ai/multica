-- How far down the retry chain the attempt holding a turn sits. Depth only
-- ever moves forward: once a retry has taken a turn over, a late frame from an
-- earlier attempt must not take it back and rewrite what the user is reading.
--
-- Its own migration rather than an edit to 500: a database that already
-- recorded 500 would never see a change made there, and every query touching
-- the column would fail on exactly the deployments this feature has already
-- been installed on. Written so a database created by 500 and one upgraded
-- through this file end up with the same schema.
ALTER TABLE channel_reply_delivery
    ADD COLUMN IF NOT EXISTS attempt_depth INTEGER NOT NULL DEFAULT 0;

ALTER TABLE channel_reply_delivery
    DROP CONSTRAINT IF EXISTS channel_reply_delivery_attempt_depth_check;

ALTER TABLE channel_reply_delivery
    ADD CONSTRAINT channel_reply_delivery_attempt_depth_check CHECK (attempt_depth >= 0);
