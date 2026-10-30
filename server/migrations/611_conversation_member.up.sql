-- Who is in a channel or direct conversation, and how far each human has
-- read. member_type is 'member' (member_id = user id) or 'agent'. Threads have
-- no member rows of their own except the humans following them, whose
-- last_read_seq tracks the thread's unread. No foreign keys by project rule.
CREATE TABLE IF NOT EXISTS conversation_member (
    conversation_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    member_type TEXT NOT NULL CHECK (member_type IN ('member', 'agent')),
    member_id UUID NOT NULL,
    last_read_seq BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
