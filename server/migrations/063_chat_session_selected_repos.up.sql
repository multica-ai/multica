-- Add selected_repo_urls to chat_session so users can pin specific repos to a conversation.
-- Empty array = no selection (default behaviour: agent uses all workspace repos).
ALTER TABLE chat_session ADD COLUMN selected_repo_urls TEXT[] NOT NULL DEFAULT '{}';
