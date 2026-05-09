-- Ship Hub Phase 4: stop treating PRs as standalone GitHub objects.
--
-- Adds the linkage spine that connects pull_request rows back into
-- Multica's existing graph (issues, agent tasks, channels, projects).
-- Phases 1–3 left the PR as an isolated GitHub mirror; with these
-- columns wired the same row knows:
--
--   - which Multica issue spawned it (originating_issue_id)
--   - which agent_task_queue row pushed the commits (originating_agent_task_id)
--   - whether merging it should auto-close that issue
--   - which conversation channel hosts the PR's discussion
--   - which other open PR it sits on top of (stack visualization)
--
-- The `source` column is a denormalized classifier computed at upsert
-- time. The frontend uses it to render the source icon (multica_agent
-- vs multica_human vs external_tool vs external_contributor) without
-- having to recompute the rule on every render.

ALTER TABLE pull_request
    ADD COLUMN originating_issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    ADD COLUMN originating_agent_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    ADD COLUMN auto_close_issue_on_merge BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN conversation_channel_id UUID REFERENCES channel(id) ON DELETE SET NULL,
    ADD COLUMN stack_parent_pr_id UUID REFERENCES pull_request(id) ON DELETE SET NULL,
    ADD COLUMN source TEXT NOT NULL DEFAULT 'external_contributor';

-- Lookups for the new "Linked PRs" panel on the issue detail page.
CREATE INDEX idx_pull_request_originating_issue
    ON pull_request(originating_issue_id)
    WHERE originating_issue_id IS NOT NULL;

-- Used by the chat-with-agent flow to enumerate "what PRs did this
-- agent task push?".
CREATE INDEX idx_pull_request_originating_agent_task
    ON pull_request(originating_agent_task_id)
    WHERE originating_agent_task_id IS NOT NULL;

-- Stack visualization queries pivot on the parent column.
CREATE INDEX idx_pull_request_stack_parent
    ON pull_request(stack_parent_pr_id)
    WHERE stack_parent_pr_id IS NOT NULL;

-- Conversation-channel lookup by PR id is rare (one read per card open)
-- but the reverse direction — "is this channel attached to a PR?" — is
-- common enough to deserve a partial index. The pull_request -> channel
-- direction is already well-indexed via the PK.
CREATE INDEX idx_pull_request_conversation_channel
    ON pull_request(conversation_channel_id)
    WHERE conversation_channel_id IS NOT NULL;
