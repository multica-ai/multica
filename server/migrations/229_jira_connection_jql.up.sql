-- Pull-based Jira sync: each connection may store a JQL filter selecting
-- which Jira issues the manual "Sync now" pull mirrors into Multica. NULL
-- (or empty) means the application default `assignee = currentUser()` —
-- i.e. the issues assigned to the account whose API token is stored.
--
-- Nullable TEXT, no FK, no index (the column is only ever read through the
-- connection row itself), so this is a safe single-statement ALTER.
ALTER TABLE jira_connection ADD COLUMN IF NOT EXISTS jql TEXT;
