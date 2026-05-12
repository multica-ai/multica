-- ROA-178: Ship Concierge — ambient listener agent in a channel.
--
-- Multica's existing channel/agent integration has two activation
-- patterns:
--   1. Member @-mentions an agent in a channel → task dispatched
--   2. Member messages an agent in a DM → every agent member of the
--      DM gets auto-dispatched (no @-mention required)
--
-- ROA-178 needs a THIRD pattern: a public/workspace-scoped channel
-- where a designated agent receives every member message ambiently,
-- without @-mention. This is the "Ship Concierge" channel — Claude
-- listens to whatever the team is discussing and responds with Ship
-- Hub queries, action confirmations, etc.
--
-- Rather than introduce a new `channel.kind` value (which would force
-- every consumer to handle one more enum case), we add a nullable
-- `ambient_listener_agent_id` column. When set:
--   * Every member-authored message in the channel triggers a task
--     for that agent. The existing dispatch path
--     (SelectAgentsForMention in message_service.go) is the single
--     point of integration — its existing DM auto-trigger condition
--     just learns one more case.
--   * Agent-authored messages do NOT re-trigger (same self-mention
--     guard as the DM path; prevents ping-pong).
--   * @-mentions still work as before for other agents — the ambient
--     listener is additive, not exclusive.
--
-- Indexed because the dispatch hot path looks up by channel id and
-- needs to read this column on every channel-message insert.

ALTER TABLE channel
    ADD COLUMN ambient_listener_agent_id UUID;

-- Partial index — only the non-null rows actually matter for the
-- dispatch check. Tiny by construction (one row per agent-channel,
-- typically <5 per workspace).
CREATE INDEX idx_channel_ambient_listener
    ON channel (ambient_listener_agent_id)
    WHERE ambient_listener_agent_id IS NOT NULL;
