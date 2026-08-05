-- One continuation per (task, member): clicking "continue in chat" twice must
-- reopen the same conversation instead of forking a second one that resumes the
-- very same provider session. Two chats driving one session is the concurrency
-- hazard the feature exists to avoid, so uniqueness is enforced in the database
-- rather than only in the handler's pre-check, which can lose a double-click race.
--
-- Scoped by creator because chat sessions are private to their creator: two
-- members continuing the same task must each get their own conversation.
-- Partial so the overwhelming majority of rows (NULL) stay out of the index.
--
-- The consequence of per-creator scoping, stated plainly so it is not mistaken
-- for an oversight and "fixed" by dropping creator_id: two members CAN hold two
-- chats naming the same provider session. That is safe because dispatch is
-- sequential per chat_session — a session has at most one active task — so the
-- two conversations take that session in turn rather than concurrently, and each
-- diverges onto its own session after its first turn. Making the index
-- (continued_from_task_id) alone would instead hand member B member A's private
-- conversation, which is a worse trade. See
-- TestContinueTaskInChat_SecondMemberGetsOwnConversation.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_session_continued_from_task
    ON chat_session(continued_from_task_id, creator_id)
    WHERE continued_from_task_id IS NOT NULL;
