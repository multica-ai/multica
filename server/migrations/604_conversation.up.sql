-- Team conversations (MUL-8357): channels, member-to-member direct messages,
-- and threads under a message of either. Agent direct chats stay on
-- chat_session.
--
-- kind = 'thread' rows hang off parent_conversation_id at root_message_id and
-- inherit the parent's access. direct_key is the two member ids sorted and
-- joined, so a pair has one direct conversation. last_message_seq orders
-- messages and drives unread against conversation_member.last_read_seq.
--
-- id is attached as the primary key in 605 from the index built CONCURRENTLY
-- in 604 (repo convention, see 574-575). No foreign keys by project rule.
CREATE TABLE IF NOT EXISTS conversation (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('channel', 'direct', 'thread')),
    name TEXT NOT NULL DEFAULT '' CHECK (char_length(name) <= 80),
    about TEXT NOT NULL DEFAULT '' CHECK (char_length(about) <= 500),
    visibility TEXT NOT NULL DEFAULT 'private' CHECK (visibility IN ('public', 'private')),
    direct_key TEXT,
    parent_conversation_id UUID,
    root_message_id UUID,
    created_by UUID NOT NULL,
    last_message_seq BIGINT NOT NULL DEFAULT 0,
    last_message_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
