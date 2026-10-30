-- A message in a conversation. seq is assigned from conversation.last_message_seq
-- under its row lock, so it is gapless per conversation and orders delivery and
-- read cursors. author_type 'system' rows (kind = 'event') report what happened
-- rather than what someone said. task_id names the agent run that produced an
-- agent reply. client_message_id makes a retried send idempotent per author.
--
-- id is attached as the primary key in 615 from the index built CONCURRENTLY
-- in 614. No foreign keys by project rule.
CREATE TABLE IF NOT EXISTS conversation_message (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    seq BIGINT NOT NULL,
    author_type TEXT NOT NULL CHECK (author_type IN ('member', 'agent', 'system')),
    author_id UUID,
    kind TEXT NOT NULL DEFAULT 'message' CHECK (kind IN ('message', 'event')),
    content TEXT NOT NULL DEFAULT '',
    task_id UUID,
    client_message_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
